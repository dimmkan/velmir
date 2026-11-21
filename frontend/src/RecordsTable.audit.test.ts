// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount } from '@vue/test-utils'
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

function recordsResponse(name: string) {
  return new Response(JSON.stringify({ records: [{ id: '1', name }], page: 1, limit: 50, has_next: false }), { status: 200 })
}

describe('records audit failure', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    window.history.replaceState({}, '', '/')
    document.body.innerHTML = ''
  })

  it('shows a committed warning, refreshes data and never automatically retries POST', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(recordsResponse('Before'))
      .mockResolvedValueOnce(new Response(JSON.stringify({
        error: { code: 'AUDIT_WRITE_FAILED', message: 'Audit failed', mutation_committed: true },
        record: { id: '1', name: 'Committed' },
      }), { status: 500 }))
      .mockResolvedValue(recordsResponse('Committed'))
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = mount(RecordsTable, {
      attachTo: document.body,
      props: {
        backendBaseURL: '', schemaName: 'public', resource, writeEnabled: true,
        readAllowed: true, createAllowed: true, updateAllowed: true, deleteAllowed: false,
      },
      global: { plugins: [createVelmirI18n()] },
    })
    await flushPromises()
    await wrapper.get('.record-controls .primary-button').trigger('click')
    await flushPromises()
    const field = document.querySelector<HTMLInputElement>('#field-name')!
    const form = document.querySelector<HTMLFormElement>('form.record-form')!
    await new DOMWrapper(field).setValue('Committed')
    await new DOMWrapper(form).trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.find('.audit-warning').exists()).toBe(true)
    expect(wrapper.text()).toContain('Изменение сохранено, но событие истории не записано')
    expect(wrapper.text()).toContain('Committed')
    expect(fetchMock).toHaveBeenCalledTimes(3)
    expect(fetchMock.mock.calls.filter(([, options]) => options?.method === 'POST')).toHaveLength(1)

    await wrapper.get('.audit-warning button').trigger('click')
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledTimes(4)
    expect(fetchMock.mock.calls.filter(([, options]) => options?.method === 'POST')).toHaveLength(1)
    wrapper.unmount()
  })
})
