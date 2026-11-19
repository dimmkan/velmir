import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  AdminRequestError,
  createAdminClient,
  validateDisplayName,
  validatePassword,
  validateRoleForm,
  validateUserForm,
} from './admin'

describe('administration form validation', () => {
  it('validates user creation and password fields on the client', () => {
    expect(validateUserForm({ email: 'invalid', display_name: '', password: 'short' })).toMatchObject({
      email: expect.any(String),
      password: expect.any(String),
    })
    expect(validateUserForm({ email: 'user@example.test', display_name: 'User', password: 'a-long-password' })).toEqual({})
    expect(validateDisplayName(' ')).toHaveProperty('display_name')
    expect(validateDisplayName('Readable name')).toEqual({})
    expect(validatePassword('short')).toHaveProperty('password')
    expect(validatePassword('replacement-password')).toEqual({})
  })

  it('validates concrete and wildcard resource permissions', () => {
    expect(validateRoleForm({
      name: 'editor',
      description: 'Editors',
      permissions: [{ schema: 'public', resource: 'books', operations: ['read', 'update'] }],
    })).toEqual({})
    expect(validateRoleForm({
      name: 'reader', description: '', permissions: [{ schema: '*', resource: '*', operations: ['read'] }],
    })).toEqual({})
    for (const permissions of [
      [{ schema: '*', resource: 'books', operations: ['read' as const] }],
      [{ schema: 'public', resource: 'books', operations: [] }],
      [{ schema: 'public', resource: 'books', operations: ['delete' as never] }],
      [
        { schema: 'public', resource: 'books', operations: ['read' as const] },
        { schema: 'public', resource: 'books', operations: ['update' as const] },
      ],
    ]) {
      expect(validateRoleForm({ name: 'editor', description: '', permissions })).toHaveProperty('permissions')
    }
    expect(validateRoleForm({ name: 'Admin', description: '', permissions: [] })).toHaveProperty('name')
  })
})

describe('administration API', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('uses same-origin cookies and in-memory CSRF for every mutation', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }))
    vi.stubGlobal('fetch', fetchMock)
    const client = createAdminClient('', () => 'memory-csrf')

    await client.setUserActive(7, false)
    await client.assignRole(7, 3)
    await client.removeRole(7, 3)
    await client.replacePermissions(3, [{ schema: '*', resource: '*', operations: ['read'] }])

    expect(fetchMock.mock.calls.map((call) => [call[0], call[1].method])).toEqual([
      ['/api/admin/users/7/status', 'PATCH'],
      ['/api/admin/users/7/roles/3', 'PUT'],
      ['/api/admin/users/7/roles/3', 'DELETE'],
      ['/api/admin/roles/3/permissions', 'PUT'],
    ])
    for (const [, request] of fetchMock.mock.calls) {
      expect(request.credentials).toBe('same-origin')
      expect(request.headers['X-CSRF-Token']).toBe('memory-csrf')
    }
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ is_active: false })
  })

  it('surfaces server validation messages and field errors', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      error: {
        code: 'INVALID_ADMIN_REQUEST',
        message: 'Role validation failed',
        field_errors: { permissions: 'Unknown operation' },
      },
    }), { status: 400 })))
    const client = createAdminClient('', () => 'csrf')

    const request = client.createRole({ name: 'bad', description: '', permissions: [] })
    await expect(request).rejects.toMatchObject({
      name: 'AdminRequestError',
      status: 400,
      code: 'INVALID_ADMIN_REQUEST',
      message: 'Role validation failed',
      fieldErrors: { permissions: 'Unknown operation' },
    })
  })

  it('creates users and roles with the expected payloads without storing credentials', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ user: { id: 2 } }), { status: 201 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ role: { id: 3 } }), { status: 201 }))
    vi.stubGlobal('fetch', fetchMock)
    const client = createAdminClient('', () => 'csrf')

    await client.createUser({ email: 'user@example.test', display_name: 'User', password: 'not-persisted-password' })
    await client.createRole({ name: 'editor', description: '', permissions: [] })

    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({
      email: 'user@example.test', display_name: 'User', password: 'not-persisted-password',
    })
    expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({ name: 'editor', description: '', permissions: [] })
    expect(client).not.toHaveProperty('password')
    expect(client).not.toHaveProperty('token')
  })

  it('normalizes failed responses into visible administration errors', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('not json', { status: 500 })))
    const client = createAdminClient('', () => 'csrf')
    await expect(client.listUsers()).rejects.toBeInstanceOf(AdminRequestError)
  })
})
