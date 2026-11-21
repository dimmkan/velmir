// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import RecordsTable from './RecordsTable.vue'
import { createVelmirI18n } from './i18n'
import type { ResourceMetadata } from './schema'

const resource: ResourceMetadata = {
  name: 'items', kind: 'table', primary_key: ['id'], foreign_keys: [],
  columns: [
    { name: 'id', data_type: 'bigint', nullable: false, default: 'nextval', primary_key: true, identity: 'always', generated: false, editable: false, enum_values: [] },
    { name: 'name', data_type: 'text', nullable: false, default: null, primary_key: false, identity: '', generated: false, editable: true, enum_values: [] },
  ],
}

let wrapper: VueWrapper | undefined

function response(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status })
}

function element<T extends HTMLElement = HTMLElement>(selector: string): DOMWrapper<T> {
  const found = document.querySelector<T>(selector)
  if (!found) throw new Error(`Missing element: ${selector}`)
  return new DOMWrapper(found)
}

async function mountTable(fetchMock: ReturnType<typeof vi.fn>) {
  vi.stubGlobal('fetch', fetchMock)
  wrapper = mount(RecordsTable, {
    attachTo: document.body,
    props: {
      backendBaseURL: '', schemaName: 'public', resource, writeEnabled: true,
      readAllowed: true, createAllowed: true, updateAllowed: true, deleteAllowed: true,
    },
    global: { plugins: [createVelmirI18n()] },
  })
  await flushPromises()
  return wrapper
}

async function chooseAction(action: string) {
  await wrapper!.get('.record-actions-trigger').trigger('click')
  await flushPromises()
  const item = Array.from(document.querySelectorAll<HTMLButtonElement>('[role="menuitem"]')).find((button) => button.textContent === action)
  if (!item) throw new Error(`Missing action: ${action}`)
  item.click()
  await flushPromises()
}

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.unstubAllGlobals()
  window.history.replaceState({}, '', '/')
  document.body.innerHTML = ''
})

describe('record modal workflows', () => {
  it('creates in a modal, blocks double submit, closes and refreshes', async () => {
    let finishCreate: ((value: Response) => void) | undefined
    const fetchMock = vi.fn().mockImplementation((input: string, options?: RequestInit) => {
      if (options?.method === 'POST') return new Promise<Response>((resolve) => { finishCreate = resolve })
      if (input.includes('/records?')) return Promise.resolve(response({ records: [{ id: '1', name: 'Before' }], page: 1, limit: 50, has_next: false }))
      return Promise.resolve(response({}, 404))
    })
    await mountTable(fetchMock)
    await wrapper!.get('.record-controls .primary-button').trigger('click')
    await flushPromises()
    expect(element('[role="dialog"]').attributes('aria-modal')).toBe('true')
    await element<HTMLInputElement>('#field-name').setValue('Created once')
    const submit = element<HTMLButtonElement>('.record-form-submit')
    await submit.trigger('click')
    await submit.trigger('click')
    expect(fetchMock.mock.calls.filter(([, options]) => options?.method === 'POST')).toHaveLength(1)
    await nextTick()
    expect(submit.attributes('disabled')).toBeDefined()

    finishCreate?.(response({ record: { id: '2', name: 'Created once' } }, 201))
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(fetchMock.mock.calls.filter(([input, options]) => String(input).includes('/records?') && !options?.method)).toHaveLength(2)
  })

  it('opens edit and history in separate modals and saves an update', async () => {
    const fetchMock = vi.fn().mockImplementation((input: string, options?: RequestInit) => {
      if (options?.method === 'PATCH') return Promise.resolve(response({ record: { id: '1', name: 'Updated' } }))
      if (input.includes('/history?')) return Promise.resolve(response({ events: [], page: 1, limit: 20, has_next: false }))
      if (input.includes('/records?')) return Promise.resolve(response({ records: [{ id: '1', name: 'Before' }], page: 1, limit: 50, has_next: false }))
      return Promise.resolve(response({}, 404))
    })
    await mountTable(fetchMock)
    await chooseAction('Редактировать')
    expect(element('[role="dialog"]').text()).toContain('Редактирование записи')
    await element<HTMLInputElement>('#field-name').setValue('Updated')
    await element('.record-form-submit').trigger('click')
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(fetchMock.mock.calls.filter(([, options]) => options?.method === 'PATCH')).toHaveLength(1)

    await chooseAction('История')
    await flushPromises()
    expect(element('[role="dialog"]').text()).toContain('История записи')
    expect(element('.history-state').text()).toContain('История изменений пока пуста')
  })
})
