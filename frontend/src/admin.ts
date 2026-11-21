import type { FieldPermissionAction, PermissionOperation, ResourcePermission } from './auth'

export interface AdminUser {
  id: number
  email: string
  display_name: string
  is_active: boolean
  roles: string[]
}

export interface AdminRole {
  id: number
  name: string
  description: string
  is_system: boolean
  permissions: ResourcePermission[]
}

export interface UserForm {
  email: string
  display_name: string
  password: string
}

export interface RoleForm {
  name: string
  description: string
  permissions: ResourcePermission[]
}

export type FormErrors = Record<string, string>

export function clonePermissions(permissions: ResourcePermission[]): ResourcePermission[] {
  return permissions.map((permission) => ({
    ...permission,
    operations: [...permission.operations],
    field_permissions: permission.field_permissions?.map((fieldPermission) => ({
      ...fieldPermission,
      columns: [...fieldPermission.columns],
    })),
  }))
}

export function fieldPermission(permission: ResourcePermission, action: FieldPermissionAction) {
  return permission.field_permissions?.find((fieldPermission) => fieldPermission.action === action) ?? {
    action,
    mode: 'unrestricted' as const,
    columns: [],
  }
}

export class AdminRequestError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code: string,
    readonly fieldErrors: FormErrors = {},
  ) {
    super(message)
    this.name = 'AdminRequestError'
  }
}

export function validateUserForm(form: UserForm): FormErrors {
  const errors: FormErrors = {}
  const email = form.email.trim()
  if (!email || email.length > 254 || /\s/.test(email)) {
    errors.email = 'validation.email'
  }
  if (form.display_name.trim().length > 200) errors.display_name = 'validation.displayNameLength'
  Object.assign(errors, validatePassword(form.password))
  return errors
}

export function validateDisplayName(displayName: string): FormErrors {
  const length = displayName.trim().length
  return length === 0 || length > 200
    ? { display_name: 'validation.displayNameRequired' }
    : {}
}

export function validatePassword(password: string): FormErrors {
  if (password.length < 12) return { password: 'validation.passwordMin' }
  if (password.length > 1024) return { password: 'validation.passwordMax' }
  return {}
}

export function validateRoleForm(form: RoleForm): FormErrors {
  const errors: FormErrors = {}
  if (!/^[a-z][a-z0-9_-]{0,99}$/.test(form.name.trim())) {
    errors.name = 'validation.roleName'
  }
  if (form.description.trim().length > 500) errors.description = 'validation.descriptionLength'
  const pairs = new Set<string>()
  for (const permission of form.permissions) {
    const schema = permission.schema.trim()
    const resource = permission.resource.trim()
    if (!schema || !resource || schema.length > 255 || resource.length > 255) {
      errors.permissions = 'validation.permissionNames'
      break
    }
    if ((schema === '*' || resource === '*') && (schema !== '*' || resource !== '*')) {
      errors.permissions = 'validation.permissionWildcard'
      break
    }
    const pair = JSON.stringify([schema, resource])
    if (pairs.has(pair)) {
      errors.permissions = 'validation.permissionDuplicates'
      break
    }
    pairs.add(pair)
    if (permission.operations.length === 0 || new Set(permission.operations).size !== permission.operations.length ||
      permission.operations.some((operation) => !['read', 'create', 'update', 'delete'].includes(operation))) {
      errors.permissions = 'validation.permissionOperations'
      break
    }
    const fields = permission.field_permissions ?? []
    const fieldActions = fields.map((field) => field.action)
    if ((schema === '*' && fields.length > 0) || fields.length > 3 || new Set(fieldActions).size !== fieldActions.length ||
      fields.some((field) => !permission.operations.includes(field.action) ||
        (field.mode === 'unrestricted' && field.columns.length > 0) ||
        (field.mode === 'allowlist' && (field.columns.length > 1000 || new Set(field.columns).size !== field.columns.length ||
          field.columns.some((column) => !column.trim() || column.length > 255))))) {
      errors.permissions = 'validation.permissionFields'
      break
    }
  }
  return errors
}

export function createAdminClient(backendBaseURL: string, getCSRFToken: () => string) {
  async function request<T>(path: string, method = 'GET', body?: unknown): Promise<T> {
    const response = await fetch(`${backendBaseURL}${path}`, {
      method,
      headers: {
        Accept: 'application/json',
        ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
        ...(method === 'GET' ? {} : { 'X-CSRF-Token': getCSRFToken() }),
      },
      credentials: 'same-origin',
      body: body === undefined ? undefined : JSON.stringify(body),
    })
    if (response.status === 204) return undefined as T
    const payload: unknown = await response.json().catch(() => null)
    if (!response.ok) throw adminError(payload, response.status)
    return payload as T
  }

  return {
    listUsers: () => request<{ users: AdminUser[] }>('/api/admin/users'),
    createUser: (form: UserForm) => request<{ user: AdminUser }>('/api/admin/users', 'POST', form),
    updateDisplayName: (userID: number, displayName: string) =>
      request<void>(`/api/admin/users/${userID}/display-name`, 'PATCH', { display_name: displayName }),
    setUserActive: (userID: number, isActive: boolean) =>
      request<void>(`/api/admin/users/${userID}/status`, 'PATCH', { is_active: isActive }),
    resetPassword: (userID: number, password: string) =>
      request<void>(`/api/admin/users/${userID}/password-reset`, 'POST', { password }),
    assignRole: (userID: number, roleID: number) =>
      request<void>(`/api/admin/users/${userID}/roles/${roleID}`, 'PUT'),
    removeRole: (userID: number, roleID: number) =>
      request<void>(`/api/admin/users/${userID}/roles/${roleID}`, 'DELETE'),
    listRoles: () => request<{ roles: AdminRole[] }>('/api/admin/roles'),
    createRole: (form: RoleForm) => request<{ role: AdminRole }>('/api/admin/roles', 'POST', form),
    updateRole: (roleID: number, name: string, description: string) =>
      request<void>(`/api/admin/roles/${roleID}`, 'PATCH', { name, description }),
    replacePermissions: (roleID: number, permissions: ResourcePermission[]) =>
      request<void>(`/api/admin/roles/${roleID}/permissions`, 'PUT', { permissions }),
  }
}

export type AdminClient = ReturnType<typeof createAdminClient>

function adminError(payload: unknown, status: number): AdminRequestError {
  if (isRecord(payload) && isRecord(payload.error)) {
    const fieldErrors: FormErrors = {}
    if (isRecord(payload.error.field_errors)) {
      for (const [field, message] of Object.entries(payload.error.field_errors)) {
        if (typeof message === 'string') fieldErrors[field] = message
      }
    }
    return new AdminRequestError(
      typeof payload.error.message === 'string' ? payload.error.message : 'Administration request failed.',
      status,
      typeof payload.error.code === 'string' ? payload.error.code : 'ADMIN_OPERATION_FAILED',
      fieldErrors,
    )
  }
  return new AdminRequestError('Administration request failed.', status, 'ADMIN_OPERATION_FAILED')
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export const permissionOperations: PermissionOperation[] = ['read', 'create', 'update', 'delete']
