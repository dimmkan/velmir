// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import App from './App.vue'
import AdministrationPage from './AdministrationPage.vue'
import AppFooter from './AppFooter.vue'
import RecordsTable from './RecordsTable.vue'
import { authState, handleUnauthorized } from './auth'
import { createVelmirI18n } from './i18n'
import type { ResourceMetadata, SchemaResponse } from './schema'

const resource: ResourceMetadata = {
  name: 'books',
  kind: 'table',
  primary_key: ['id'],
  foreign_keys: [],
  columns: [
    { name: 'id', data_type: 'bigint', nullable: false, default: null, primary_key: true, identity: 'always', generated: false, editable: false, enum_values: [] },
    { name: 'title', data_type: 'text', nullable: false, default: null, primary_key: false, identity: '', generated: false, editable: true, enum_values: [] },
  ],
}

const schema: SchemaResponse = {
  write_enabled: true,
  schemas: [{ name: 'public', resources: [resource] }],
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function mockRecords() {
  return vi.fn().mockImplementation(() => Promise.resolve(jsonResponse({
    records: [{ id: '1', title: 'Database entry' }],
    page: 1,
    limit: 50,
    has_next: false,
  })))
}

const RecordsHost = defineComponent({
  components: { RecordsTable, AppFooter },
  props: {
    readAllowed: { type: Boolean, default: true },
    createAllowed: { type: Boolean, default: true },
    updateAllowed: { type: Boolean, default: true },
    deleteAllowed: { type: Boolean, default: false },
  },
  data: () => ({ resource }),
  template: `
    <div>
      <RecordsTable
        backend-base-u-r-l=""
        schema-name="public"
        :resource="resource"
        :write-enabled="true"
        :read-allowed="readAllowed"
        :create-allowed="createAllowed"
        :update-allowed="updateAllowed"
        :delete-allowed="deleteAllowed"
      />
      <AppFooter />
    </div>
  `,
})

const AdministrationHost = defineComponent({
  components: { AdministrationPage, AppFooter },
  template: '<div><AdministrationPage backend-base-u-r-l="" /><AppFooter /></div>',
})

async function switchToEnglish(wrapper: ReturnType<typeof mount>) {
  await wrapper.findAll('.language-switch button')[1].trigger('click')
  await nextTick()
}

beforeEach(() => {
  window.localStorage.clear()
  window.history.replaceState({}, '', '/')
  handleUnauthorized()
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  window.localStorage.clear()
  window.history.replaceState({}, '', '/')
  handleUnauthorized()
})

describe('localized records and permissions', () => {
  it('translates records and keeps the shared footer available', async () => {
    vi.stubGlobal('fetch', mockRecords())
    const instance = createVelmirI18n()
    const wrapper = mount(RecordsHost, { global: { plugins: [instance] } })
    await flushPromises()

    expect(wrapper.text()).toContain('Поиск')
    expect(wrapper.text()).toContain('Создать запись')
    expect(wrapper.text()).toContain('Строк на странице')
    expect(wrapper.text()).toContain('Database entry')
    expect(wrapper.find('.app-footer').exists()).toBe(true)

    await switchToEnglish(wrapper)

    expect(wrapper.text()).toContain('Search')
    expect(wrapper.text()).toContain('Create record')
    expect(wrapper.text()).toContain('Rows per page')
    expect(wrapper.text()).toContain('Database entry')
    expect(wrapper.text()).not.toContain('records.')
    wrapper.unmount()
  })

  it('keeps create and edit controls hidden without RBAC grants in both languages', async () => {
    vi.stubGlobal('fetch', mockRecords())
    const instance = createVelmirI18n()
    const wrapper = mount(RecordsHost, {
      props: { readAllowed: false, createAllowed: false, updateAllowed: false },
      global: { plugins: [instance] },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('У вас доступ только для чтения')
    expect(wrapper.find('.record-controls .primary-button').exists()).toBe(false)
    expect(wrapper.find('.record-action-cell').exists()).toBe(false)

    await switchToEnglish(wrapper)

    expect(wrapper.text()).toContain('You have read-only access')
    expect(wrapper.find('.record-controls .primary-button').exists()).toBe(false)
    expect(wrapper.find('.record-action-cell').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Create record')
    wrapper.unmount()
  })
})

describe('localized Administration', () => {
  it('translates Administration and preserves its footer and controls', async () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: string) => {
      if (input.endsWith('/api/admin/users')) return Promise.resolve(jsonResponse({ users: [] }))
      if (input.endsWith('/api/admin/roles')) return Promise.resolve(jsonResponse({ roles: [] }))
      return Promise.resolve(jsonResponse({}, 404))
    }))
    const instance = createVelmirI18n()
    const wrapper = mount(AdministrationHost, { global: { plugins: [instance] } })
    await flushPromises()

    expect(wrapper.get('#administration-title').text()).toBe('Администрирование')
    expect(wrapper.text()).toContain('Пользователи')
    expect(wrapper.text()).toContain('Создать пользователя')
    expect(wrapper.find('.app-footer').exists()).toBe(true)

    await switchToEnglish(wrapper)

    expect(wrapper.get('#administration-title').text()).toBe('Administration')
    expect(wrapper.text()).toContain('Users')
    expect(wrapper.text()).toContain('Create user')
    expect(wrapper.get('.language-switch button[aria-pressed="true"]').text()).toBe('English')
    wrapper.unmount()
  })
})

describe('application locale integration', () => {
  it('does not repeat authentication and preserves RBAC hiding when switching languages', async () => {
    const fetchMock = vi.fn().mockImplementation((input: string) => {
      if (input.endsWith('/api/auth/me')) {
        return Promise.resolve(jsonResponse({
          user: { id: 1, email: 'viewer@example.test', display_name: 'Viewer' },
          csrf_token: 'test-csrf',
          permissions: [{ schema: '*', resource: '*', operations: ['read'] }],
          can_admin: false,
        }))
      }
      if (input.endsWith('/readyz')) {
        return Promise.resolve(jsonResponse({ status: 'ready', admin_database: 'ready', target_database: 'ready' }))
      }
      if (input.endsWith('/api/schema')) return Promise.resolve(jsonResponse(schema))
      if (input.includes('/records?')) {
        return Promise.resolve(jsonResponse({ records: [], page: 1, limit: 50, has_next: false }))
      }
      return Promise.resolve(jsonResponse({}, 404))
    })
    vi.stubGlobal('fetch', fetchMock)
    const instance = createVelmirI18n()
    const wrapper = mount(App, { global: { plugins: [instance] } })
    await flushPromises()
    await flushPromises()

    expect(authState.status).toBe('authenticated')
    expect(wrapper.find('.administration-nav').exists()).toBe(false)
    expect(wrapper.find('.record-controls .primary-button').exists()).toBe(false)
    expect(wrapper.find('.app-footer').exists()).toBe(true)
    const requestCount = fetchMock.mock.calls.length
    const authRequests = fetchMock.mock.calls.filter(([url]) => String(url).includes('/api/auth/')).length

    await switchToEnglish(wrapper)

    expect(wrapper.text()).toContain('Search')
    expect(wrapper.find('.administration-nav').exists()).toBe(false)
    expect(wrapper.find('.record-controls .primary-button').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Create record')
    expect(fetchMock).toHaveBeenCalledTimes(requestCount)
    expect(fetchMock.mock.calls.filter(([url]) => String(url).includes('/api/auth/'))).toHaveLength(authRequests)
    expect(authState.status).toBe('authenticated')
    wrapper.unmount()
  })
})
