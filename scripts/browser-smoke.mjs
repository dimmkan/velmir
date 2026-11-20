const baseURL = process.env.SMOKE_BASE_URL || 'https://localhost:18443'
const cdpURL = process.env.SMOKE_CDP_URL || 'http://127.0.0.1:19222'
const login = process.env.SMOKE_ADMIN_LOGIN || 'smoke-admin@example.test'
const password = process.env.SMOKE_ADMIN_PASSWORD || 'H2-smoke-only-password-2026'

const target = await fetch(`${cdpURL}/json/new?about:blank`, { method: 'PUT' }).then((response) => {
  if (!response.ok) throw new Error(`CDP target creation failed: ${response.status}`)
  return response.json()
})
const socket = new WebSocket(target.webSocketDebuggerUrl)
await new Promise((resolve, reject) => {
  socket.addEventListener('open', resolve, { once: true })
  socket.addEventListener('error', reject, { once: true })
})

let nextID = 1
const pending = new Map()
const eventWaiters = new Map()
const browserErrors = []
socket.addEventListener('message', (event) => {
  const message = JSON.parse(event.data)
  if (message.id) {
    const waiter = pending.get(message.id)
    if (!waiter) return
    pending.delete(message.id)
    if (message.error) waiter.reject(new Error(`${message.error.message}: ${message.error.data || ''}`))
    else waiter.resolve(message.result)
    return
  }
  if (message.method === 'Runtime.exceptionThrown') {
    browserErrors.push(message.params.exceptionDetails.text)
  }
  if (message.method === 'Runtime.consoleAPICalled' && message.params.type === 'error') {
    browserErrors.push(message.params.args.map((item) => item.value || item.description || '').join(' '))
  }
  if (message.method === 'Log.entryAdded' && message.params.entry.level === 'error' && message.params.entry.source !== 'network') {
    browserErrors.push(message.params.entry.text)
  }
  const waiters = eventWaiters.get(message.method)
  if (waiters) {
    eventWaiters.delete(message.method)
    for (const resolve of waiters) resolve(message.params)
  }
})

function send(method, params = {}) {
  const id = nextID++
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject })
    socket.send(JSON.stringify({ id, method, params }))
  })
}

function assert(condition, message) {
  if (!condition) throw new Error(message)
}

async function evaluate(expression) {
  const result = await send('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true })
  if (result.exceptionDetails) throw new Error(result.exceptionDetails.exception?.description || result.exceptionDetails.text)
  return result.result.value
}

async function navigate(path) {
  const loaded = new Promise((resolve) => {
    eventWaiters.set('Page.loadEventFired', [resolve])
  })
  await send('Page.navigate', { url: `${baseURL}${path}` })
  await Promise.race([loaded, new Promise((_, reject) => setTimeout(() => reject(new Error(`Navigation timeout: ${path}`)), 15000))])
  await new Promise((resolve) => setTimeout(resolve, 500))
}

async function waitFor(expression, timeout = 10000) {
  const deadline = Date.now() + timeout
  while (Date.now() < deadline) {
    if (await evaluate(expression)) return
    await new Promise((resolve) => setTimeout(resolve, 100))
  }
  throw new Error(`Browser condition timeout: ${expression}`)
}

async function api(path, method = 'GET', body, csrfToken) {
  const expression = `(async () => {
    const headers = {}
    if (${JSON.stringify(body !== undefined)}) headers['Content-Type'] = 'application/json'
    if (${JSON.stringify(csrfToken || '')}) headers['X-CSRF-Token'] = ${JSON.stringify(csrfToken || '')}
    const response = await fetch(${JSON.stringify(`${baseURL}${path}`)}, {
      method: ${JSON.stringify(method)}, headers, credentials: 'include',
      body: ${JSON.stringify(body === undefined ? null : JSON.stringify(body))}
    })
    const text = await response.text()
    let data = null
    try { data = text ? JSON.parse(text) : null } catch { data = text }
    return { status: response.status, data, headers: {
      csp: response.headers.get('Content-Security-Policy'),
      hsts: response.headers.get('Strict-Transport-Security'),
      nosniff: response.headers.get('X-Content-Type-Options'),
      frame: response.headers.get('X-Frame-Options'),
      referrer: response.headers.get('Referrer-Policy'),
      permissions: response.headers.get('Permissions-Policy')
    } }
  })()`
  return evaluate(expression)
}

try {
  await send('Page.enable')
  await send('Runtime.enable')
  await send('Network.enable')
  await send('Log.enable')
  await send('Network.clearBrowserCookies')
  await navigate('/login')
  await waitFor(`document.querySelector('form') !== null`)
  assert(await evaluate(`document.documentElement.lang`) === 'ru', 'Russian is not the default locale')
  assert(await evaluate(`[...document.images].some((image) => image.complete && image.naturalWidth > 0)`), 'Login logo did not load')
  assert((await api('/favicon.svg')).status === 200, 'SVG favicon failed')
  assert((await api('/favicon-32x32.png')).status === 200, 'PNG favicon failed')

  const health = await api('/healthz')
  assert(health.status === 200, 'Health endpoint failed')
  assert(health.headers.nosniff === 'nosniff' && health.headers.frame === 'DENY', 'Security headers are incomplete')
  assert(health.headers.referrer === 'no-referrer' && health.headers.permissions, 'Privacy headers are incomplete')
  assert(health.headers.hsts === 'max-age=31536000', 'Production HSTS is missing')
  assert(health.headers.csp.includes(`script-src 'self'`) && !health.headers.csp.includes('unsafe-eval'), 'Unsafe script CSP')
  assert(!health.headers.csp.includes(`script-src 'self' 'unsafe-inline'`), 'Inline scripts are allowed')

  assert((await api('/api/auth/me')).status === 401, 'Anonymous /me should return 401')
  const signedIn = await api('/api/auth/login', 'POST', { email: login, password })
  assert(signedIn.status === 200, `Login failed: ${signedIn.status} ${JSON.stringify(signedIn.data)}`)
  const csrfToken = signedIn.data.csrf_token
  const cookies = await send('Network.getAllCookies')
  const sessionCookie = cookies.cookies.find((cookie) => cookie.name === 'velmir_session')
  const csrfCookie = cookies.cookies.find((cookie) => cookie.name === 'velmir_csrf')
  assert(sessionCookie?.secure && sessionCookie.httpOnly && sessionCookie.sameSite === 'Lax', 'Session cookie flags are incorrect')
  assert(csrfCookie?.secure && !csrfCookie.httpOnly && csrfCookie.sameSite === 'Lax', 'CSRF cookie flags are incorrect')
  assert((await api('/api/auth/me')).status === 200, 'Authenticated /me failed')

  const schema = await api('/api/schema')
  assert(schema.status === 200, 'Schema failed')
  const publicSchema = schema.data.schemas.find((item) => item.name === 'public')
  const books = publicSchema?.resources.find((item) => item.name === 'books')
  const authors = publicSchema?.resources.find((item) => item.name === 'authors')
  assert(books?.foreign_keys.length > 0 && authors?.reverse_relations.length > 0, 'Relations metadata is missing')
  const bookList = await api('/api/resources/public/books/records?limit=5')
  const authorList = await api('/api/resources/public/authors/records?limit=5')
  assert(bookList.status === 200 && bookList.data.records.length > 0, 'Books list failed')
  assert(authorList.status === 200 && authorList.data.records.length > 0, 'Authors list failed')
  const resolved = await api('/api/resources/public/books/relations/resolve', 'POST', {
    relations: [{ name: books.foreign_keys[0].name, keys: [{ id: bookList.data.records[0].author_id }] }],
  })
  assert(resolved.status === 200, 'Relation resolution failed')

  assert((await api('/api/resources/public/categories/records', 'POST', { values: { name: 'CSRF must fail' } })).status === 403, 'CSRF check was bypassed')
  const categoryName = `H2 smoke ${Date.now()}`
  const created = await api('/api/resources/public/categories/records', 'POST', { values: { name: categoryName } }, csrfToken)
  assert(created.status === 201, `Create failed: ${JSON.stringify(created.data)}`)
  const category = created.data.record
  const updated = await api(`/api/resources/public/categories/records/${encodeURIComponent(JSON.stringify({ id: String(category.id) }))}`, 'PATCH', { values: { name: `${categoryName} updated` } }, csrfToken)
  assert(updated.status === 200, 'Update failed')
  const history = await api(`/api/resources/public/categories/records/${encodeURIComponent(JSON.stringify({ id: String(category.id) }))}/history`)
  assert(history.status === 200 && history.data.events.length >= 2, 'Audit history failed')
  const junction = await api('/api/resources/public/book_categories/records', 'POST', {
    values: { book_id: String(bookList.data.records[0].id), category_id: String(category.id) },
  }, csrfToken)
  assert(junction.status === 201, 'Many-to-many create failed')
  const junctionKey = encodeURIComponent(JSON.stringify({ book_id: String(bookList.data.records[0].id), category_id: String(category.id) }))
  assert((await api(`/api/resources/public/book_categories/records/${junctionKey}`, 'DELETE', undefined, csrfToken)).status === 204, 'Many-to-many delete failed')
  assert((await api(`/api/resources/public/categories/records/${encodeURIComponent(JSON.stringify({ id: String(category.id) }))}`, 'DELETE', undefined, csrfToken)).status === 204, 'Category delete failed')
  assert((await api('/api/does-not-exist')).status === 404, 'API 404 failed')

  await navigate('/?schema=public&resource=books')
  await waitFor(`document.querySelector('.record-delete-button') !== null`)
  await evaluate(`[...document.querySelectorAll('button')].find((button) => button.textContent.includes('Создать запись')).click()`)
  await waitFor(`document.querySelector('.record-form-panel .relation-form-field') !== null`)
  await evaluate(`[...document.querySelectorAll('.record-form-panel button')].find((button) => button.textContent.includes('Отмена')).click()`)
  await waitFor(`document.querySelector('.record-form-panel') === null`)
  await evaluate(`document.querySelector('.record-delete-button').click()`)
  await waitFor(`document.querySelector('[role="dialog"]') !== null`)
  await evaluate(`[...document.querySelectorAll('[role="dialog"] button')].find((button) => button.textContent.includes('Отмена')).click()`)

  await evaluate(`document.querySelectorAll('.language-switch button')[1].click()`)
  await waitFor(`document.documentElement.lang === 'en'`)
  await evaluate(`document.querySelectorAll('.language-switch button')[0].click()`)
  await waitFor(`document.documentElement.lang === 'ru'`)

  assert((await api('/api/auth/logout', 'POST', undefined, csrfToken)).status === 204, 'Logout failed')
  assert((await api('/api/auth/me')).status === 401, 'Logout did not revoke the session')
  await navigate('/login')
  await waitFor(`document.querySelector('form') !== null`)
  assert(browserErrors.length === 0, `Browser/CSP errors: ${browserErrors.join(' | ')}`)
  console.log('Browser smoke passed: HTTPS UI, cookies, CSRF, CRUD, relations, history, dialogs, SPA, ru/en, favicon; no CSP violations or browser exceptions')
} finally {
  await fetch(`${cdpURL}/json/close/${target.id}`).catch(() => {})
  socket.close()
}
