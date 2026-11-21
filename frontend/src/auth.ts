import { reactive } from 'vue'
import { errorTranslationKey } from './i18n'

export interface AuthUser {
  id: number
  email: string
  display_name: string
}

export type PermissionOperation = 'read' | 'create' | 'update' | 'delete'

export type FieldPermissionAction = 'create' | 'update'
export type FieldPermissionMode = 'unrestricted' | 'allowlist'

export interface FieldPermission {
  action: FieldPermissionAction
  mode: FieldPermissionMode
  columns: string[]
}

export interface ResourcePermission {
  schema: string
  resource: string
  operations: PermissionOperation[]
  field_permissions?: FieldPermission[]
}

export type AuthStatus = 'loading' | 'authenticated' | 'anonymous' | 'error'

export interface AuthState {
  status: AuthStatus
  user: AuthUser | null
  permissions: ResourcePermission[]
  can_admin: boolean
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
  can_admin: boolean
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
    can_admin: false,
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
      state.can_admin = false
      csrfToken = ''
      state.status = 'error'
      state.error = 'errors.network'
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
        const code = readErrorCode(body, response.status === 401 ? 'INVALID_CREDENTIALS' : 'LOGIN_FAILED')
        state.loginError = errorTranslationKey(code)
        return false
      }
      if (!isAuthResponse(body)) {
        state.loginError = 'errors.invalidLoginResponse'
        return false
      }
      authenticate(body)
      return true
    } catch {
      state.loginError = 'errors.network'
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
        state.logoutError = errorTranslationKey(readErrorCode(body, 'LOGOUT_FAILED'), response.status)
        return false
      }
      expire()
      return true
    } catch {
      state.logoutError = 'errors.logoutNetwork'
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
      field_permissions: permission.field_permissions?.map((fieldPermission) => ({
        ...fieldPermission,
        columns: [...fieldPermission.columns],
      })),
    }))
    state.can_admin = body.can_admin
    state.status = 'authenticated'
    state.error = ''
    state.loginError = ''
    state.logoutError = ''
  }

  function expire() {
    csrfToken = ''
    state.user = null
    state.permissions = []
    state.can_admin = false
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
    typeof value.can_admin === 'boolean' &&
    Array.isArray(value.permissions) &&
    value.permissions.every(isResourcePermission)
  )
}

function isResourcePermission(value: unknown): value is ResourcePermission {
  if (!isRecord(value) || typeof value.schema !== 'string' || typeof value.resource !== 'string' || !Array.isArray(value.operations)) return false
  if (!value.operations.every((operation) => operation === 'read' || operation === 'create' || operation === 'update' || operation === 'delete')) return false
  if (value.field_permissions === undefined) return true
  return Array.isArray(value.field_permissions) && value.field_permissions.every((fieldPermission) =>
    isRecord(fieldPermission) &&
    (fieldPermission.action === 'create' || fieldPermission.action === 'update') &&
    (fieldPermission.mode === 'unrestricted' || fieldPermission.mode === 'allowlist') &&
    Array.isArray(fieldPermission.columns) &&
    fieldPermission.columns.every((column) => typeof column === 'string'),
  )
}

function readErrorCode(value: unknown, fallback: string) {
  if (isRecord(value) && isRecord(value.error) && typeof value.error.code === 'string') {
    return value.error.code
  }
  return fallback
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
