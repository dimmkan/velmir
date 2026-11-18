import { reactive } from 'vue'

export interface AuthUser {
  id: number
  email: string
  display_name: string
}

export type PermissionOperation = 'read' | 'create' | 'update'

export interface ResourcePermission {
  schema: string
  resource: string
  operations: PermissionOperation[]
}

export type AuthStatus = 'loading' | 'authenticated' | 'anonymous' | 'error'

export interface AuthState {
  status: AuthStatus
  user: AuthUser | null
  permissions: ResourcePermission[]
  error: string
  loginPending: boolean
  loginError: string
  logoutPending: boolean
  logoutError: string
}

interface AuthResponse {
  user: AuthUser
  csrf_token: string
  permissions: ResourcePermission[]
}

export function hasPermission(
  permissions: ResourcePermission[],
  schema: string,
  resource: string,
  operation: PermissionOperation,
): boolean {
  return permissions.some((permission) =>
    (permission.schema === '*' || permission.schema === schema) &&
    (permission.resource === '*' || permission.resource === resource) &&
    permission.operations.includes(operation),
  )
}

export function createAuthStore(backendBaseURL = '') {
  const state = reactive<AuthState>({
    status: 'loading',
    user: null,
    permissions: [],
    error: '',
    loginPending: false,
    loginError: '',
    logoutPending: false,
    logoutError: '',
  })
  let csrfToken = ''
  let initialization: Promise<void> | null = null

  async function initialize(): Promise<void> {
    if (initialization) return initialization
    initialization = runInitialization()
    try {
      await initialization
    } finally {
      initialization = null
    }
  }

  async function runInitialization() {
    state.status = 'loading'
    state.error = ''
    try {
      const response = await fetch(`${backendBaseURL}/api/auth/me`, {
        headers: { Accept: 'application/json' },
        credentials: 'same-origin',
      })
      if (response.status === 401) {
        expire()
        return
      }
      const body: unknown = await response.json()
      if (!response.ok || !isAuthResponse(body)) throw new Error('Could not verify the session')
      authenticate(body)
    } catch {
      state.user = null
      state.permissions = []
      csrfToken = ''
      state.status = 'error'
      state.error = 'Could not contact the backend. Check that it is running and try again.'
    }
  }

  async function login(email: string, password: string): Promise<boolean> {
    if (state.loginPending) return false
    state.loginPending = true
    state.loginError = ''
    try {
      const response = await fetch(`${backendBaseURL}/api/auth/login`, {
        method: 'POST',
        headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
        credentials: 'same-origin',
        body: JSON.stringify({ email, password }),
      })
      const body: unknown = await response.json()
      if (!response.ok) {
        state.loginError = readErrorMessage(body, response.status === 401
          ? 'Invalid email or password.'
          : 'Could not sign in. Try again.')
        return false
      }
      if (!isAuthResponse(body)) {
        state.loginError = 'The backend returned an invalid login response.'
        return false
      }
      authenticate(body)
      return true
    } catch {
      state.loginError = 'Could not contact the backend. Check your connection and try again.'
      return false
    } finally {
      state.loginPending = false
      password = ''
    }
  }

  async function logout(): Promise<boolean> {
    if (state.logoutPending) return false
    state.logoutPending = true
    state.logoutError = ''
    try {
      const response = await fetch(`${backendBaseURL}/api/auth/logout`, {
        method: 'POST',
        headers: { Accept: 'application/json', 'X-CSRF-Token': csrfToken },
        credentials: 'same-origin',
      })
      if (!response.ok && response.status !== 401) {
        const body: unknown = await response.json()
        state.logoutError = readErrorMessage(body, 'Could not sign out. Try again.')
        return false
      }
      expire()
      return true
    } catch {
      state.logoutError = 'Could not contact the backend. You are still signed in.'
      return false
    } finally {
      state.logoutPending = false
    }
  }

  function authenticate(body: AuthResponse) {
    csrfToken = body.csrf_token
    state.user = body.user
    state.permissions = body.permissions.map((permission) => ({
      ...permission,
      operations: [...permission.operations],
    }))
    state.status = 'authenticated'
    state.error = ''
    state.loginError = ''
    state.logoutError = ''
  }

  function expire() {
    csrfToken = ''
    state.user = null
    state.permissions = []
    state.status = 'anonymous'
    state.error = ''
    state.loginError = ''
    state.logoutError = ''
  }

  return {
    state,
    initialize,
    login,
    logout,
    expire,
    getCSRFToken: () => csrfToken,
    can: (schema: string, resource: string, operation: PermissionOperation) =>
      hasPermission(state.permissions, schema, resource, operation),
  }
}

const authStore = createAuthStore((import.meta.env.VITE_BACKEND_BASE_URL ?? '').replace(/\/+$/, ''))

export const authState = authStore.state
export const initializeAuth = authStore.initialize
export const login = authStore.login
export const logout = authStore.logout
export const handleUnauthorized = authStore.expire
export const getCSRFToken = authStore.getCSRFToken
export const can = authStore.can

function isAuthResponse(value: unknown): value is AuthResponse {
  if (!isRecord(value) || !isRecord(value.user) || typeof value.csrf_token !== 'string' || value.csrf_token === '') return false
  return (
    typeof value.user.id === 'number' &&
    Number.isSafeInteger(value.user.id) &&
    typeof value.user.email === 'string' &&
    typeof value.user.display_name === 'string' &&
    Array.isArray(value.permissions) &&
    value.permissions.every(isResourcePermission)
  )
}

function isResourcePermission(value: unknown): value is ResourcePermission {
  if (!isRecord(value) || typeof value.schema !== 'string' || typeof value.resource !== 'string' || !Array.isArray(value.operations)) return false
  return value.operations.every((operation) => operation === 'read' || operation === 'create' || operation === 'update')
}

function readErrorMessage(value: unknown, fallback: string) {
  if (isRecord(value) && isRecord(value.error) && typeof value.error.message === 'string') {
    return value.error.message
  }
  return fallback
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
