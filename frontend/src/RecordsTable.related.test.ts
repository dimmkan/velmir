// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import RecordsTable from './RecordsTable.vue'
import { authState } from './auth'
import { createVelmirI18n } from './i18n'
import type { RecordRow } from './records'
import type { ColumnMetadata, ResourceMetadata } from './schema'

function column(name: string, dataType = 'text'): ColumnMetadata {
  return {
    name, data_type: dataType, nullable: true, default: null, primary_key: name === 'id',
    identity: '', generated: false, editable: true, enum_values: [],
  }
}

const authors: ResourceMetadata = {
  name: 'authors', kind: 'table', primary_key: ['id'], foreign_keys: [],
  columns: [column('id', 'bigint'), column('name')],
}
const books: ResourceMetadata = {
  name: 'books', kind: 'table', primary_key: ['id'], foreign_keys: [{
    name: 'books_author_fk', columns: ['author_id'], referenced_schema: 'public',
    referenced_table: 'authors', referenced_columns: ['id'],
  }],
  columns: [column('id', 'bigint'), column('author_id', 'bigint'), column('title')],
}
const parents: ResourceMetadata = {
  name: 'parents', kind: 'table', primary_key: ['tenant_id', 'external_id'], foreign_keys: [],
  columns: [column('tenant_id', 'bigint'), column('external_id', 'uuid'), column('name')],
}
const children: ResourceMetadata = {
  name: 'children', kind: 'table', primary_key: ['id'], foreign_keys: [{
    name: 'children_parent_fk', columns: ['tenant_id', 'parent_id'], referenced_schema: 'public',
    referenced_table: 'parents', referenced_columns: ['tenant_id', 'external_id'],
  }],
  columns: [column('id', 'bigint'), column('tenant_id', 'bigint'), column('parent_id', 'uuid'), column('title')],
}

let wrapper: VueWrapper | null = null

function recordsResponse(records: RecordRow[]): Response {
  return new Response(JSON.stringify({ records, page: 1, limit: 50, has_next: false }), { status: 200 })
}

function mountTable(
  resource: ResourceMetadata,
  referenced: ResourceMetadata,
  records: RecordRow[],
  relatedRecords: RecordRow[],
  resolveStatus = 200,
) {
  const fetchMock = vi.fn().mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
    if (init?.method === 'POST') {
      if (resolveStatus !== 200) return new Response(JSON.stringify({ error: { code: 'FORBIDDEN', message: 'Denied' } }), { status: resolveStatus })
      const body = JSON.parse(String(init.body))
      return new Response(JSON.stringify({
        relations: body.relations.map((relation: { name: string }) => ({ name: relation.name, records: relatedRecords })),
      }), { status: 200 })
    }
    return recordsResponse(records)
  })
  vi.stubGlobal('fetch', fetchMock)
  wrapper = mount(RecordsTable, {
    attachTo: document.body,
    props: {
      backendBaseURL: '', schemaName: 'public', resource, schemas: [{ name: 'public', resources: [resource, referenced] }],
      writeEnabled: false, readAllowed: true, createAllowed: false, updateAllowed: false, deleteAllowed: false,
    },
    global: { plugins: [createVelmirI18n()] },
  })
  return { wrapper, fetchMock }
}

beforeEach(() => {
  authState.status = 'authenticated'
  authState.permissions = [{ schema: '*', resource: '*', operations: ['read'] }]
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
  window.history.replaceState({}, '', '/')
})

describe('RecordsTable related labels', () => {
  it('uses one deduplicated batch, distinguishes identical labels and localizes null', async () => {
    const { wrapper, fetchMock } = mountTable(books, authors, [
      { id: '1', author_id: '1', title: 'One' },
      { id: '2', author_id: '1', title: 'Two' },
      { id: '3', author_id: '2', title: 'Three' },
      { id: '4', author_id: null, title: 'No author' },
    ], [{ id: '1', name: 'Same author' }, { id: '2', name: 'Same author' }])
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledTimes(2)
    const batch = fetchMock.mock.calls.find(([, init]) => init?.method === 'POST')
    expect(JSON.parse(String(batch?.[1]?.body)).relations[0].keys).toEqual([{ id: '1' }, { id: '2' }])
    expect(wrapper.findAll('.related-record-label').map((item) => item.text())).toEqual([
      'Same author', 'Same author', 'Same author',
    ])
    const keys = wrapper.findAll('.related-record-cell code').map((item) => item.text())
    expect(keys).toEqual(['id=1', 'id=1', 'id=2'])
    expect(wrapper.find('.null-relation').text()).toBe('Нет связанной записи')
    expect(wrapper.text()).toContain('No author')
  })

  it('shows every composite key part next to the shared label', async () => {
    const tenantID = '9223372036854775807'
    const uuid = '00112233-4455-6677-8899-aabbccddeeff'
    const { wrapper } = mountTable(children, parents, [
      { id: '1', tenant_id: tenantID, parent_id: uuid, title: 'Child' },
    ], [{ tenant_id: tenantID, external_id: uuid, name: 'Parent' }])
    await flushPromises()

    expect(wrapper.get('.related-record-label').text()).toBe('Parent')
    expect(wrapper.get('.related-record-cell code').text()).toBe(`tenant_id=${tenantID} · external_id=${uuid}`)
    expect(wrapper.get('.related-record-cell').attributes('title')).toContain(uuid)
  })

  it('keeps the main records and raw FK fallback on resolve 403/error', async () => {
    const { wrapper, fetchMock } = mountTable(books, authors, [
      { id: '1', author_id: '9223372036854775807', title: 'Still visible' },
    ], [], 403)
    await flushPromises()

    expect(wrapper.text()).toContain('Still visible')
    expect(wrapper.get('.related-record-raw').text()).toBe('9223372036854775807')
    expect(wrapper.get('.related-record-cell').attributes('title')).toContain('исходное значение FK')
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(authState.status).toBe('authenticated')
  })

  it('makes no related request and exposes no label without referenced read', async () => {
    authState.permissions = [{ schema: 'public', resource: 'books', operations: ['read'] }]
    const { wrapper, fetchMock } = mountTable(books, authors, [
      { id: '1', author_id: '1', title: 'Raw book' },
    ], [{ id: '1', name: 'Hidden author' }])
    await flushPromises()

    expect(wrapper.get('.related-record-raw').text()).toBe('1')
    expect(wrapper.text()).not.toContain('Hidden author')
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls.every(([, init]) => init?.method !== 'POST')).toBe(true)
  })
})
