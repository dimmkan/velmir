import { afterEach, describe, expect, it, vi } from 'vitest'
import { createAuthStore, hasPermission, type AuthUser, type ResourcePermission } from './auth'

const user: AuthUser = { id: 1, email: 'admin@example.test', display_name: 'Administrator' }
const csrfToken = 'csrf-token-in-memory-only'
const adminPermissions: ResourcePermission[] = [{ schema: '*', resource: '*', operations: ['read', 'create', 'update'] }]

function authResponse(status = 200, permissions = adminPermissions, canAdmin = true) {
  return new Response(JSON.stringify({ user, csrf_token: csrfToken, permissions, can_admin: canAdmin }), {
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
    expect(store.state.error).toBe('errors.network')
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
    expect(store.state.loginError).toBe('errors.codes.INVALID_CREDENTIALS')
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


describe('effective permissions', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('allows admin supported operations and hides them from viewer and a user without roles', async () => {
    const viewerPermissions: ResourcePermission[] = [{ schema: '*', resource: '*', operations: ['read'] }]
    for (const test of [
      { permissions: adminPermissions, create: true, update: true },
      { permissions: viewerPermissions, create: false, update: false },
      { permissions: [], create: false, update: false },
    ]) {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue(authResponse(200, test.permissions)))
      const store = createAuthStore()
      await store.initialize()

      expect(store.state.status).toBe('authenticated')
      expect(store.state.permissions).toEqual(test.permissions)
      expect(store.can('public', 'items', 'read')).toBe(test.permissions.length > 0)
      expect(store.can('public', 'items', 'create')).toBe(test.create)
      expect(store.can('public', 'items', 'update')).toBe(test.update)
    }
  })

  it('unions roles and matches resource-level and wildcard grants', () => {
    const permissions: ResourcePermission[] = [
      { schema: 'public', resource: 'items', operations: ['read'] },
      { schema: 'public', resource: 'items', operations: ['update'] },
      { schema: 'archive', resource: '*', operations: ['read'] },
    ]

    expect(hasPermission(permissions, 'public', 'items', 'read')).toBe(true)
    expect(hasPermission(permissions, 'public', 'items', 'update')).toBe(true)
    expect(hasPermission(permissions, 'public', 'items', 'create')).toBe(false)
    expect(hasPermission(permissions, 'archive', 'old_items', 'read')).toBe(true)
    expect(hasPermission(permissions, 'private', 'secrets', 'read')).toBe(false)
  })
})
