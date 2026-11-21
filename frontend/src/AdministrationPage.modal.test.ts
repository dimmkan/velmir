// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import AdministrationPage from './AdministrationPage.vue'
import { createVelmirI18n } from './i18n'
import type { AdminRole } from './admin'
import type { ResourceMetadata } from './schema'

let wrapper: VueWrapper | undefined
const books: ResourceMetadata = {
  name: 'books', kind: 'table' as const, primary_key: ['id'], foreign_keys: [],
  columns: [
    { name: 'id', data_type: 'bigint', nullable: false, default: null, primary_key: true, identity: '', generated: false, editable: false, enum_values: [] },
    { name: 'title', data_type: 'text', nullable: false, default: null, primary_key: false, identity: '', generated: false, editable: true, enum_values: [] },
  ],
}
const roles: AdminRole[] = [
  { id: 1, name: 'admin', description: '', is_system: true, permissions: [] },
  { id: 2, name: 'reader', description: '', is_system: false, permissions: [{ schema: 'public', resource: 'books', operations: ['read'], field_permissions: [{ action: 'read', mode: 'unrestricted', columns: [] }] }] },
]

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status })
}

async function mountAdmin() {
  const fetchMock = vi.fn().mockImplementation((input: string, options?: RequestInit) => {
    if (input.endsWith('/api/admin/users')) return Promise.resolve(json({ users: [] }))
    if (input.endsWith('/api/admin/roles')) return Promise.resolve(json({ roles }))
    if (options?.method === 'PATCH' || options?.method === 'PUT') return Promise.resolve(new Response(null, { status: 204 }))
    return Promise.resolve(json({}, 404))
  })
  vi.stubGlobal('fetch', fetchMock)
  wrapper = mount(AdministrationPage, {
    props: { backendBaseURL: '', schemas: [{ name: 'public', resources: [books] }] },
    global: { plugins: [createVelmirI18n()], stubs: { teleport: true } },
  })
  await flushPromises()
  await wrapper.findAll('[role="tab"]')[1].trigger('click')
  return fetchMock
}

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('role permissions modal', () => {
  it('opens only editable roles and saves an explicit empty read allowlist', async () => {
    const fetchMock = await mountAdmin()
    const configure = wrapper!.findAll('.role-card button')
    expect(configure).toHaveLength(1)
    await configure[0].trigger('click')
    expect(wrapper!.get('[role="dialog"]').text()).toContain('Права роли reader')
    const readRadios = wrapper!.findAll('.field-permission-editor input[type="radio"]')
    expect((readRadios[0].element as HTMLInputElement).checked).toBe(true)
    await readRadios[1].setValue(true)
    expect(wrapper!.get('.empty-allowlist-warning').text()).toContain('Только primary key')
    await wrapper!.get('.modal-footer .primary-button').trigger('click')
    await flushPromises()
    const put = fetchMock.mock.calls.find(([, options]) => options?.method === 'PUT')
    expect(put).toBeDefined()
    expect(JSON.parse(String(put![1]?.body)).permissions[0].field_permissions).toEqual([{ action: 'read', mode: 'allowlist', columns: [] }])
  })

  it('keeps wildcard unrestricted inside the modal', async () => {
    roles[1].permissions = [{ schema: '*', resource: '*', operations: ['read', 'create', 'update', 'delete'], field_permissions: [] }]
    await mountAdmin()
    await wrapper!.get('.role-card button').trigger('click')
    expect(wrapper!.get('.wildcard-field-note').text()).toContain('Wildcard *.*')
    expect(wrapper!.find('.field-permission-editor').exists()).toBe(false)
    roles[1].permissions = [{ schema: 'public', resource: 'books', operations: ['read'], field_permissions: [{ action: 'read', mode: 'unrestricted', columns: [] }] }]
  })
})
