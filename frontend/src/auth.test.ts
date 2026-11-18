import { afterEach, describe, expect, it, vi } from 'vitest'
import { createAuthStore, type AuthUser } from './auth'

const user: AuthUser = { id: 1, email: 'admin@example.test', display_name: 'Administrator' }
const csrfToken = 'csrf-token-in-memory-only'

function authResponse(status = 200) {
  return new Response(JSON.stringify({ user, csrf_token: csrfToken }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('auth state', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('loads the current user and CSRF token with me at startup', async () => {
    const fetchMock = vi.fn().mockResolvedValue(authResponse())
    vi.stubGlobal('fetch', fetchMock)
    const store = createAuthStore()

    expect(store.state.status).toBe('loading')
    await store.initialize()

    expect(store.state.status).toBe('authenticated')
    expect(store.state.user).toEqual(user)
    expect(store.getCSRFToken()).toBe(csrfToken)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock).toHaveBeenCalledWith('/api/auth/me', expect.objectContaining({ credentials: 'same-origin' }))
  })

  it('treats a startup 401 as anonymous without retrying in a loop', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      error: { code: 'UNAUTHENTICATED', message: 'Authentication required' },
    }), { status: 401 }))
    vi.stubGlobal('fetch', fetchMock)
    const store = createAuthStore()

    await store.initialize()
    store.expire()
    store.expire()

    expect(store.state.status).toBe('anonymous')
    expect(store.state.user).toBeNull()
    expect(store.getCSRFToken()).toBe('')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('shows a recoverable loading error when me fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')))
    const store = createAuthStore()

    await store.initialize()

    expect(store.state.status).toBe('error')
    expect(store.state.error).toContain('Could not contact')
  })

  it('logs in without persisting the password and keeps CSRF in memory', async () => {
    const fetchMock = vi.fn().mockResolvedValue(authResponse())
    vi.stubGlobal('fetch', fetchMock)
    const store = createAuthStore()

    await expect(store.login('admin@example.test', 'secret-password')).resolves.toBe(true)

    expect(store.state.status).toBe('authenticated')
    expect(store.state.loginError).toBe('')
    expect(store.getCSRFToken()).toBe(csrfToken)
    const [, request] = fetchMock.mock.calls[0]
    expect(JSON.parse(request.body)).toEqual({ email: 'admin@example.test', password: 'secret-password' })
    expect(request.credentials).toBe('same-origin')
    expect(store.state).not.toHaveProperty('password')
  })

  it('shows the generic login error and remains anonymous', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      error: { code: 'INVALID_CREDENTIALS', message: 'Invalid email or password' },
    }), { status: 401 })))
    const store = createAuthStore()

    await expect(store.login('unknown@example.test', 'wrong')).resolves.toBe(false)

    expect(store.state.status).toBe('loading')
    expect(store.state.loginError).toBe('Invalid email or password')
    expect(store.getCSRFToken()).toBe('')
  })

  it('sends CSRF on logout and clears local auth state', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(authResponse())
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
    vi.stubGlobal('fetch', fetchMock)
    const store = createAuthStore()
    await store.initialize()

    await expect(store.logout()).resolves.toBe(true)

    expect(fetchMock).toHaveBeenLastCalledWith('/api/auth/logout', expect.objectContaining({
      method: 'POST',
      headers: expect.objectContaining({ 'X-CSRF-Token': csrfToken }),
      credentials: 'same-origin',
    }))
    expect(store.state.status).toBe('anonymous')
    expect(store.state.user).toBeNull()
    expect(store.getCSRFToken()).toBe('')
  })

  it('clears auth state when logout receives 401', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(authResponse())
      .mockResolvedValueOnce(new Response(null, { status: 401 }))
    vi.stubGlobal('fetch', fetchMock)
    const store = createAuthStore()
    await store.initialize()

    await expect(store.logout()).resolves.toBe(true)
    expect(store.state.status).toBe('anonymous')
    expect(store.getCSRFToken()).toBe('')
  })

  it('handles a 401 from any protected request by expiring the session', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(authResponse()))
    const store = createAuthStore()
    await store.initialize()

    store.expire()

    expect(store.state.status).toBe('anonymous')
    expect(store.state.user).toBeNull()
    expect(store.getCSRFToken()).toBe('')
  })
})
