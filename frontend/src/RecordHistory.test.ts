// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import RecordHistory from './RecordHistory.vue'
import AppFooter from './AppFooter.vue'
import { authState } from './auth'
import { createVelmirI18n } from './i18n'
import type { HistoryEvent } from './history'
import type { ResourceMetadata } from './schema'

const resource: ResourceMetadata = {
  name: 'items', kind: 'table', primary_key: ['id'], foreign_keys: [],
  columns: [
    { name: 'id', data_type: 'bigint', nullable: false, default: null, primary_key: true, identity: '', generated: false, editable: false, enum_values: [] },
    { name: 'amount', data_type: 'numeric', nullable: true, default: null, primary_key: false, identity: '', generated: false, editable: true, enum_values: [] },
    { name: 'active', data_type: 'boolean', nullable: true, default: null, primary_key: false, identity: '', generated: false, editable: true, enum_values: [] },
    { name: 'published_on', data_type: 'date', nullable: true, default: null, primary_key: false, identity: '', generated: false, editable: true, enum_values: [] },
    { name: 'external_id', data_type: 'uuid', nullable: true, default: null, primary_key: false, identity: '', generated: false, editable: true, enum_values: [] },
    { name: 'payload', data_type: 'jsonb', nullable: true, default: null, primary_key: false, identity: '', generated: false, editable: true, enum_values: [] },
    { name: 'data', data_type: 'bytea', nullable: true, default: null, primary_key: false, identity: '', generated: false, editable: false, enum_values: [] },
    { name: 'optional', data_type: 'text', nullable: true, default: null, primary_key: false, identity: '', generated: false, editable: true, enum_values: [] },
  ],
}

const Host = defineComponent({
  components: { RecordHistory, AppFooter },
  data: () => ({ resource }),
  template: '<div><RecordHistory backend-base-u-r-l="" schema-name="public" :resource="resource" record-key="{&quot;id&quot;:&quot;1&quot;}" /><AppFooter /></div>',
})

function historyEvent(action: 'create' | 'update'): HistoryEvent {
  const before = action === 'update' ? {
    id: '1', amount: '1.00', active: false, published_on: '2025-01-01', external_id: '00112233-4455-6677-8899-aabbccddeeff', payload: { edition: 1 }, data: 'AAH/', optional: null,
  } : null
  const after = {
    id: '9223372036854775807', amount: '12345678901234567890.0001', active: true, published_on: '2025-01-02', external_id: '00112233-4455-6677-8899-aabbccddeeff', payload: { edition: 2 }, data: 'AAH/', optional: null,
  }
  return {
    id: `event-${action}`, actor: { id: 1, email: 'admin@example.test', display_name: 'Administrator' },
    timestamp: '2025-01-02T03:04:05Z', action, schema: 'public', resource: 'items', record_key: { id: '1' }, before, after,
  }
}

function response(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function mountHistory() {
  const instance = createVelmirI18n()
  return mount(Host, { global: { plugins: [instance] } })
}

beforeEach(() => {
  window.localStorage.clear()
  authState.status = 'authenticated'
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  window.localStorage.clear()
  authState.status = 'authenticated'
})

describe('record history panel', () => {
  it('renders create values, exact numbers and supported PostgreSQL representations', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response({ events: [historyEvent('create')], page: 1, limit: 20, has_next: false })))
    const wrapper = mountHistory()
    await flushPromises()

    expect(wrapper.text()).toContain('Создано')
    expect(wrapper.text()).toContain('Administrator')
    expect(wrapper.text()).toContain('9223372036854775807')
    expect(wrapper.text()).toContain('12345678901234567890.0001')
    expect(wrapper.text()).toContain('true')
    expect(wrapper.text()).toContain('null')
    expect(wrapper.text()).toContain('00112233-4455-6677-8899-aabbccddeeff')
    expect(wrapper.text()).toContain('{"edition":2}')
    expect(wrapper.text()).toContain('base64:AAH/')
    expect(wrapper.text()).toContain(new Intl.DateTimeFormat('ru', { dateStyle: 'medium', timeZone: 'UTC' }).format(new Date('2025-01-02T00:00:00Z')))
    expect(wrapper.find('.history-pagination').attributes('aria-label')).toBe('Пагинация истории')
    wrapper.unmount()
  })

  it('renders update before and after values', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response({ events: [historyEvent('update')], page: 1, limit: 20, has_next: false })))
    const wrapper = mountHistory()
    await flushPromises()

    const amountChange = wrapper.findAll('.history-change').find((element) => element.text().includes('amount'))
    expect(amountChange?.text()).toContain('1.00')
    expect(amountChange?.text()).toContain('12345678901234567890.0001')
    expect(amountChange?.text()).toContain('До')
    expect(amountChange?.text()).toContain('После')
    expect(wrapper.find('.history-arrow').text()).toBe('→')
    wrapper.unmount()
  })

  it('translates the panel through the language switch without reloading or refetching', async () => {
    const fetchMock = vi.fn().mockResolvedValue(response({ events: [historyEvent('update')], page: 1, limit: 20, has_next: false }))
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = mountHistory()
    await flushPromises()
    expect(wrapper.text()).toContain('Изменённые поля')

    await wrapper.findAll('.language-switch button')[1].trigger('click')

    expect(wrapper.text()).toContain('Record history')
    expect(wrapper.text()).toContain('Changed fields')
    expect(wrapper.text()).toContain('Before')
    expect(wrapper.text()).toContain('After')
    expect(wrapper.text()).not.toContain('history.')
    expect(fetchMock).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('handles loading and empty history', async () => {
    let resolveRequest: ((value: Response) => void) | undefined
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() => new Promise<Response>((resolve) => { resolveRequest = resolve })))
    const wrapper = mountHistory()
    expect(wrapper.text()).toContain('Загрузка истории…')
    resolveRequest!(response({ events: [], page: 1, limit: 20, has_next: false }))
    await flushPromises()
    expect(wrapper.text()).toContain('История изменений пока пуста')
    expect(wrapper.find('.history-error').exists()).toBe(false)
    wrapper.unmount()
  })

  it.each([
    { status: 401, code: 'UNAUTHENTICATED', text: 'Требуется аутентификация', auth: 'anonymous' },
    { status: 403, code: 'FORBIDDEN', text: 'нет права на чтение истории', auth: 'authenticated' },
    { status: 500, code: 'HISTORY_QUERY_FAILED', text: 'Не удалось прочитать историю', auth: 'authenticated' },
  ])('handles $status without an inappropriate login redirect', async ({ status, code, text, auth }) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response({ error: { code, message: 'Server message' } }, status)))
    const wrapper = mountHistory()
    await flushPromises()

    expect(wrapper.find('.history-error').exists()).toBe(true)
    expect(wrapper.text()).toContain(text)
    expect(authState.status).toBe(auth)
    expect(window.location.pathname).not.toBe('/login')
    expect(wrapper.get('button.secondary-button').text()).toBe('Повторить')
    wrapper.unmount()
  })
})
