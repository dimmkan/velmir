// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import RecordForm from './RecordForm.vue'
import { authState, handleUnauthorized } from './auth'
import { createVelmirI18n } from './i18n'
import type { RecordRow } from './records'
import type { ColumnMetadata, ResourceMetadata, SchemaMetadata } from './schema'

function column(name: string, dataType: string, overrides: Partial<ColumnMetadata> = {}): ColumnMetadata {
  return {
    name,
    data_type: dataType,
    nullable: false,
    default: null,
    primary_key: false,
    identity: '',
    generated: false,
    editable: true,
    enum_values: [],
    ...overrides,
  }
}

function localID(): ColumnMetadata {
  return column('id', 'bigint', {
    default: 'nextval', primary_key: true, identity: 'always', editable: false,
  })
}

const authors: ResourceMetadata = {
  name: 'authors',
  kind: 'table',
  primary_key: ['id'],
  foreign_keys: [],
  columns: [column('id', 'bigint', { primary_key: true }), column('name', 'text')],
}

const books: ResourceMetadata = {
  name: 'books',
  kind: 'table',
  primary_key: ['id'],
  foreign_keys: [{
    name: 'books_author_fk',
    columns: ['author_id'],
    referenced_schema: 'public',
    referenced_table: 'authors',
    referenced_columns: ['id'],
  }],
  columns: [localID(), column('author_id', 'bigint'), column('title', 'text')],
}

const parents: ResourceMetadata = {
  name: 'parents',
  kind: 'table',
  primary_key: ['tenant_id', 'external_id'],
  foreign_keys: [],
  columns: [
    column('tenant_id', 'bigint', { primary_key: true }),
    column('external_id', 'uuid', { primary_key: true }),
    column('name', 'text'),
  ],
}

const children: ResourceMetadata = {
  name: 'children',
  kind: 'table',
  primary_key: ['id'],
  foreign_keys: [{
    name: 'children_parent_fk',
    columns: ['tenant_id', 'parent_id'],
    referenced_schema: 'public',
    referenced_table: 'parents',
    referenced_columns: ['tenant_id', 'external_id'],
  }],
  columns: [
    localID(),
    column('tenant_id', 'bigint', { nullable: true }),
    column('parent_id', 'uuid', { nullable: true }),
    column('title', 'text'),
  ],
}

const schemas: SchemaMetadata[] = [{ name: 'public', resources: [books, authors, children, parents] }]
let wrapper: VueWrapper | null = null

function recordsResponse(records: RecordRow[], url: string): Response {
  const query = new URL(url, 'http://localhost').searchParams
  return new Response(JSON.stringify({
    records,
    page: Number(query.get('page')),
    limit: Number(query.get('limit')),
    has_next: false,
  }), { status: 200 })
}

function mountForm(
  resource: ResourceMetadata,
  relatedRecords: RecordRow[],
  mode: 'create' | 'edit' = 'create',
  record?: RecordRow,
) {
  const fetchMock = vi.fn().mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    if (init?.method === 'POST' || init?.method === 'PATCH') {
      return new Response(JSON.stringify({ record: record ?? { id: '1' } }), { status: init.method === 'POST' ? 201 : 200 })
    }
    return recordsResponse(relatedRecords, url)
  })
  vi.stubGlobal('fetch', fetchMock)
  wrapper = mount(RecordForm, {
    attachTo: document.body,
    props: {
      backendBaseURL: '', schemaName: 'public', resource, schemas, mode, record,
    },
    global: { plugins: [createVelmirI18n()] },
  })
  return { wrapper, fetchMock }
}

async function chooseFirstOption() {
  await wrapper!.get('[role="combobox"]').trigger('focus')
  document.querySelector<HTMLButtonElement>('.relation-option')!.click()
  await nextTick()
}

beforeEach(() => {
  window.localStorage.clear()
  authState.status = 'authenticated'
  authState.permissions = [{ schema: '*', resource: '*', operations: ['read', 'create', 'update'] }]
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  handleUnauthorized()
  document.body.innerHTML = ''
})

describe('record form relation payloads', () => {
  it('uses one simple FK selector and preserves a bigint key as a string', async () => {
    const id = '9223372036854775807'
    const { wrapper, fetchMock } = mountForm(books, [{ id, name: 'Author' }])
    await flushPromises()
    await chooseFirstOption()
    await wrapper.get('#field-title').setValue('Title')
    await wrapper.get('form.record-form').trigger('submit.prevent')
    await flushPromises()

    const mutation = fetchMock.mock.calls.find(([, init]) => init?.method === 'POST')
    expect(JSON.parse(String(mutation?.[1]?.body))).toEqual({ values: { author_id: id, title: 'Title' } })
    expect(wrapper.findAll('[role="combobox"]')).toHaveLength(1)
  })

  it('uses one composite FK selector and submits all bigint/UUID parts together', async () => {
    const tenantID = '9223372036854775807'
    const uuid = '00112233-4455-6677-8899-aabbccddeeff'
    const { wrapper, fetchMock } = mountForm(children, [{ tenant_id: tenantID, external_id: uuid, name: 'Parent' }])
    await flushPromises()

    expect(wrapper.findAll('[role="combobox"]')).toHaveLength(1)
    expect(wrapper.find('#field-parent_id').exists()).toBe(false)
    await chooseFirstOption()
    await wrapper.get('#field-title').setValue('Child')
    await wrapper.get('form.record-form').trigger('submit.prevent')
    await flushPromises()

    const mutation = fetchMock.mock.calls.find(([, init]) => init?.method === 'POST')
    expect(JSON.parse(String(mutation?.[1]?.body))).toEqual({
      values: { tenant_id: tenantID, parent_id: uuid, title: 'Child' },
    })
  })

  it('clears all parts of a nullable relation as JSON null in the existing PATCH payload', async () => {
    const { wrapper, fetchMock } = mountForm(
      { ...books, columns: books.columns.map((item) => item.name === 'author_id' ? { ...item, nullable: true } : item) },
      [{ id: '1', name: 'Author' }],
      'edit',
      { id: '9', author_id: '1', title: 'Title' },
    )
    await flushPromises()
    await wrapper.get('.relation-clear').trigger('click')
    await wrapper.get('form.record-form').trigger('submit.prevent')
    await flushPromises()

    const mutation = fetchMock.mock.calls.find(([, init]) => init?.method === 'PATCH')
    expect(JSON.parse(String(mutation?.[1]?.body))).toEqual({ values: { author_id: null } })
    expect(String(mutation?.[1]?.body)).not.toContain('"null"')
  })

  it('blocks an incomplete initial composite relation instead of sending a partial PATCH', async () => {
    const { wrapper, fetchMock } = mountForm(children, [], 'edit', {
      id: '9', tenant_id: '1', parent_id: null, title: 'Child',
    })
    await flushPromises()
    expect((wrapper.get('[role="combobox"]').element as HTMLInputElement).value).toContain('tenant_id=1')
    await wrapper.get('form.record-form').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.text()).toContain('Заполните все части составной связи или очистите её целиком')
    expect(fetchMock.mock.calls.some(([, init]) => init?.method === 'PATCH')).toBe(false)
    expect(fetchMock.mock.calls.every(([url]) => !String(url).includes('filter='))).toBe(true)
  })
})

describe('record form relation permissions and read-only state', () => {
  it('uses a raw input and makes no related requests without read on the referenced resource', async () => {
    authState.permissions = [{ schema: 'public', resource: 'books', operations: ['read', 'create', 'update'] }]
    const { wrapper, fetchMock } = mountForm(books, [{ id: '1', name: 'Hidden label' }])
    await flushPromises()

    expect(wrapper.find('[role="combobox"]').exists()).toBe(false)
    expect(wrapper.get('#field-author_id').attributes('role')).toBeUndefined()
    expect(wrapper.text()).toContain('Связанный ресурс недоступен для чтения')
    expect(wrapper.text()).not.toContain('Hidden label')
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('keeps a non-editable FK read-only without a selector or related requests', async () => {
    const readOnlyBooks: ResourceMetadata = {
      ...books,
      primary_key: ['id', 'author_id'],
      columns: books.columns.map((item) => item.name === 'author_id' ? { ...item, primary_key: true } : item),
    }
    const { wrapper, fetchMock } = mountForm(readOnlyBooks, [], 'edit', {
      id: '9', author_id: '1', title: 'Title',
    })
    await flushPromises()

    expect(wrapper.find('[role="combobox"]').exists()).toBe(false)
    expect(wrapper.get('#field-author_id').attributes('disabled')).toBeDefined()
    expect((wrapper.get('#field-author_id').element as HTMLInputElement).value).toBe('1')
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
