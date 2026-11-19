// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import RecordsTable from './RecordsTable.vue'
import { createVelmirI18n } from './i18n'
import { authState } from './auth'
import type { RecordRow } from './records'
import type { ResourceMetadata } from './schema'

const resource: ResourceMetadata = {
  name: 'items', kind: 'table', primary_key: ['id'], foreign_keys: [],
  columns: [
    { name: 'id', data_type: 'bigint', nullable: false, default: 'nextval', primary_key: true, identity: 'always', generated: false, editable: false, enum_values: [] },
    { name: 'name', data_type: 'text', nullable: false, default: null, primary_key: false, identity: '', generated: false, editable: true, enum_values: [] },
  ],
}

const compositeResource: ResourceMetadata = {
  ...resource,
  name: 'links',
  primary_key: ['tenant_id', 'id'],
  columns: [
    { ...resource.columns[0], name: 'tenant_id' },
    resource.columns[0],
    resource.columns[1],
  ],
}

function recordsResponse(records: RecordRow[] = [{ id: '1', name: 'Before' }], page = 1) {
  return new Response(JSON.stringify({ records, page, limit: 50, has_next: false }), { status: 200 })
}

function deleteResponse(body: unknown = null, status = 204) {
  return new Response(body === null ? null : JSON.stringify(body), { status })
}

const wrappers: VueWrapper[] = []

async function mountTable(fetchMock: ReturnType<typeof vi.fn>, overrides: Record<string, unknown> = {}, metadata = resource) {
  const i18n = createVelmirI18n()
  const wrapper = mount(RecordsTable, {
    attachTo: document.body,
    props: {
      backendBaseURL: '', schemaName: 'public', resource: metadata, writeEnabled: true,
      readAllowed: true, createAllowed: false, updateAllowed: false, deleteAllowed: true,
      ...overrides,
    },
    global: { plugins: [i18n] },
  })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, i18n }
}

async function openDialog(wrapper: VueWrapper) {
  await wrapper.get('.record-delete-button').trigger('click')
  await nextTick()
}

afterEach(() => {
  while (wrappers.length) wrappers.pop()?.unmount()
  vi.unstubAllGlobals()
  window.history.replaceState({}, '', '/')
  authState.status = 'authenticated'
  document.body.innerHTML = ''
})

describe('record deletion controls and confirmation', () => {
  it('shows delete only with permission, writer, a table and a primary key', async () => {
    for (const overrides of [{}, { deleteAllowed: false }, { writeEnabled: false }]) {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue(recordsResponse()))
      const { wrapper } = await mountTable(fetch as ReturnType<typeof vi.fn>, overrides)
      expect(wrapper.find('.record-delete-button').exists()).toBe(Object.keys(overrides).length === 0)
    }

    const unsupportedResources: ResourceMetadata[] = [{ ...resource, kind: 'view' }, { ...resource, primary_key: [] }]
    for (const metadata of unsupportedResources) {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue(recordsResponse()))
      const { wrapper } = await mountTable(fetch as ReturnType<typeof vi.fn>, {}, metadata)
      expect(wrapper.find('.record-delete-button').exists()).toBe(false)
    }
  })

  it('opens an accessible composite-key dialog, traps focus and closes with Escape without DELETE', async () => {
    const fetchMock = vi.fn().mockResolvedValue(recordsResponse([{ tenant_id: '7', id: '9', name: 'Composite' }]))
    vi.stubGlobal('fetch', fetchMock)
    const { wrapper } = await mountTable(fetchMock, {}, compositeResource)
    const trigger = wrapper.get('.record-delete-button')
    await openDialog(wrapper)

    const dialog = wrapper.get('[role="dialog"]')
    expect(dialog.attributes('aria-modal')).toBe('true')
    expect(dialog.attributes('aria-label')).toBe('Подтверждение удаления записи')
    expect(dialog.text()).toContain('public.links')
    expect(dialog.text()).toContain('{"tenant_id":"7","id":"9"}')
    expect(dialog.text()).toContain('необратимо')
    expect(document.activeElement?.textContent).toBe('Отмена')

    await dialog.trigger('keydown', { key: 'Tab', shiftKey: true })
    expect(document.activeElement?.textContent).toBe('Удалить безвозвратно')
    await dialog.trigger('keydown', { key: 'Tab' })
    expect(document.activeElement?.textContent).toBe('Отмена')
    await dialog.trigger('keydown', { key: 'Escape' })
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(document.activeElement).toBe(trigger.element)
    expect(fetchMock.mock.calls.filter(([, options]) => options?.method === 'DELETE')).toHaveLength(0)
  })

  it('blocks repeat submission, deletes once, refreshes and shows success', async () => {
    let finishDelete: (() => void) | undefined
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(recordsResponse())
      .mockImplementationOnce(() => new Promise<Response>((resolve) => {
        finishDelete = () => resolve(deleteResponse())
      }))
      .mockResolvedValue(recordsResponse([]))
    vi.stubGlobal('fetch', fetchMock)
    const { wrapper } = await mountTable(fetchMock)
    await openDialog(wrapper)
    const confirm = wrapper.get('.delete-dialog .danger-button')
    await confirm.trigger('click')
    expect(confirm.attributes('disabled')).toBeDefined()
    expect(wrapper.get('.delete-dialog').text()).toContain('Удаление…')
    await confirm.trigger('click')
    expect(fetchMock.mock.calls.filter(([, options]) => options?.method === 'DELETE')).toHaveLength(1)

    finishDelete?.()
    await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(wrapper.get('.delete-success').text()).toBe('Запись успешно удалена.')
    expect(wrapper.text()).not.toContain('Before')
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('returns to the previous page after deleting its last record', async () => {
    window.history.replaceState({}, '', '/?page=2')
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(recordsResponse([{ id: '1', name: 'Last' }], 2))
      .mockResolvedValueOnce(deleteResponse())
      .mockResolvedValue(recordsResponse([{ id: '0', name: 'Previous page' }], 1))
    vi.stubGlobal('fetch', fetchMock)
    const { wrapper } = await mountTable(fetchMock)
    await openDialog(wrapper)
    await wrapper.get('.delete-dialog .danger-button').trigger('click')
    await flushPromises()

    expect(new URLSearchParams(window.location.search).get('page')).toBe('1')
    expect(fetchMock.mock.calls[2][0]).toContain('page=1')
    expect(wrapper.text()).toContain('Previous page')
  })
})

describe('record deletion failures', () => {
  it.each([
    [401, 'UNAUTHENTICATED', 'Требуется аутентификация'],
    [403, 'FORBIDDEN', 'У вас нет права'],
    [404, 'RECORD_NOT_FOUND', 'Запись не найдена'],
    [400, 'FOREIGN_KEY_VIOLATION', 'ограничения целостности'],
  ])('shows localized HTTP %s %s state without retry', async (status, code, message) => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(recordsResponse())
      .mockResolvedValue(deleteResponse({ error: { code, message: 'Server message' } }, status))
    vi.stubGlobal('fetch', fetchMock)
    const { wrapper } = await mountTable(fetchMock)
    await openDialog(wrapper)
    await wrapper.get('.delete-dialog .danger-button').trigger('click')
    await flushPromises()

    expect(wrapper.get('.delete-error').text()).toContain(message)
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
    expect(fetchMock.mock.calls.filter(([, options]) => options?.method === 'DELETE')).toHaveLength(1)
    expect(wrapper.find('.delete-success').exists()).toBe(false)
  })

  it('warns about committed delete without audit, refreshes once and never retries DELETE', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(recordsResponse())
      .mockResolvedValueOnce(deleteResponse({ error: { code: 'AUDIT_WRITE_FAILED', message: 'Audit failed', mutation_committed: true } }, 500))
      .mockResolvedValue(recordsResponse([]))
    vi.stubGlobal('fetch', fetchMock)
    const { wrapper, i18n } = await mountTable(fetchMock)
    await openDialog(wrapper)
    await wrapper.get('.delete-dialog .danger-button').trigger('click')
    await flushPromises()

    expect(wrapper.get('.audit-warning').text()).toContain('Запись удалена, но событие истории могло не записаться')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(wrapper.find('.delete-success').exists()).toBe(false)
    expect(fetchMock.mock.calls.filter(([, options]) => options?.method === 'DELETE')).toHaveLength(1)

    i18n.global.locale.value = 'en'
    await nextTick()
    expect(wrapper.get('.audit-warning').text()).toContain('The record was deleted, but its history event may not have been recorded')
    await wrapper.get('.audit-warning button').trigger('click')
    await flushPromises()
    expect(fetchMock.mock.calls.filter(([, options]) => options?.method === 'DELETE')).toHaveLength(1)
  })

  it('switches confirmation language without submitting DELETE', async () => {
    const fetchMock = vi.fn().mockResolvedValue(recordsResponse())
    vi.stubGlobal('fetch', fetchMock)
    const { wrapper, i18n } = await mountTable(fetchMock)
    await openDialog(wrapper)
    i18n.global.locale.value = 'en'
    await nextTick()

    expect(wrapper.get('[role="dialog"]').text()).toContain('Delete this record?')
    expect(wrapper.get('[role="dialog"]').text()).toContain('This action is irreversible')
    expect(wrapper.get('[role="dialog"]').attributes('aria-label')).toBe('Confirm record deletion')
    expect(fetchMock.mock.calls.filter(([, options]) => options?.method === 'DELETE')).toHaveLength(0)
  })
})
