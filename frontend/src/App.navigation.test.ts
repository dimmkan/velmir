// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import App from './App.vue'
import { createVelmirI18n } from './i18n'
import { handleUnauthorized } from './auth'

const resource = {
  name: 'items', kind: 'table' as const, primary_key: ['id'], foreign_keys: [],
  creatable_fields: [], updatable_fields: [], can_create: false, can_update: false,
  columns: [{ name: 'id', data_type: 'bigint', nullable: false, default: null, primary_key: true, identity: '', generated: false, editable: false, enum_values: [] }],
}
const schema = {
  write_enabled: true,
  schemas: [
    { name: 'public', resources: [resource] },
    { name: 'audit', resources: [{ ...resource, name: 'events' }] },
  ],
}
let wrapper: VueWrapper | undefined

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status })
}

function mockApi() {
  vi.stubGlobal('fetch', vi.fn().mockImplementation((input: string) => {
    if (input.endsWith('/api/auth/me')) return Promise.resolve(json({
      user: { id: 1, email: 'admin@example.test', display_name: 'Admin' }, csrf_token: 'test',
      permissions: [{ schema: '*', resource: '*', operations: ['read'] }], can_admin: true,
    }))
    if (input.endsWith('/readyz')) return Promise.resolve(json({ status: 'ready', admin_database: 'ready', target_database: 'ready' }))
    if (input.endsWith('/api/schema')) return Promise.resolve(json(schema))
    if (input.includes('/records?')) return Promise.resolve(json({ records: [], page: 1, limit: 50, has_next: false }))
    return Promise.resolve(json({}, 404))
  }))
}

async function mountApp() {
  wrapper = mount(App, { global: { plugins: [createVelmirI18n()] } })
  await flushPromises()
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  window.localStorage.clear()
  window.history.replaceState({}, '', '/')
  mockApi()
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.unstubAllGlobals()
  window.localStorage.clear()
  handleUnauthorized()
})

describe('navigation persistence', () => {
  it('collapses schemas independently and restores both schema and sidebar state', async () => {
    const app = await mountApp()
    const schemaButtons = app.findAll('.schema-toggle')
    expect(schemaButtons).toHaveLength(2)
    expect(schemaButtons[0].attributes('aria-expanded')).toBe('true')
    expect(schemaButtons[1].attributes('aria-expanded')).toBe('true')
    await schemaButtons[0].trigger('click')
    expect(app.findAll('.schema-toggle')[0].attributes('aria-expanded')).toBe('false')
    expect(app.findAll('.schema-toggle')[1].attributes('aria-expanded')).toBe('true')
    expect(app.get('#schema-resources-0').attributes('style')).toContain('display: none')
    expect(app.get('#schema-resources-1').attributes('style') ?? '').not.toContain('display: none')
    expect(JSON.parse(window.localStorage.getItem('velmir_collapsed_schemas') ?? '{}')).toEqual({ public: true })

    await app.get('.sidebar-toggle').trigger('click')
    expect(app.get('.sidebar').classes()).toContain('collapsed')
    expect(window.localStorage.getItem('velmir_sidebar_collapsed')).toBe('true')
    app.unmount()
    wrapper = undefined

    const restored = await mountApp()
    expect(restored.findAll('.schema-toggle')[0].attributes('aria-expanded')).toBe('false')
    expect(restored.findAll('.schema-toggle')[1].attributes('aria-expanded')).toBe('true')
    expect(restored.get('.sidebar').classes()).toContain('collapsed')
    await restored.get('.sidebar-toggle').trigger('click')
    expect(restored.get('.sidebar').classes()).not.toContain('collapsed')
    expect(window.localStorage.getItem('velmir_sidebar_collapsed')).toBe('false')
  })

  it('keeps the direct administration route while schema metadata loads', async () => {
    window.history.replaceState({}, '', '/administration')
    const app = await mountApp()
    expect(window.location.pathname).toBe('/administration')
    expect(app.get('#administration-title').text()).toBe('Администрирование')
    expect(app.find('.records-section').exists()).toBe(false)
  })
})
