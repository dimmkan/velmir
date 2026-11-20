// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import RecordForm from './RecordForm.vue'
import { authState, handleUnauthorized } from './auth'
import { createVelmirI18n } from './i18n'
import type { ColumnMetadata, ResourceMetadata } from './schema'

function column(name: string, dataType = 'text', overrides: Partial<ColumnMetadata> = {}): ColumnMetadata {
  return {
    name, data_type: dataType, nullable: false, default: null, primary_key: false,
    identity: '', generated: false, editable: true, enum_values: [], ...overrides,
  }
}

const parentFK = {
  name: 'links_parent_fk', columns: ['parent_id'], referenced_schema: 'public',
  referenced_table: 'parents', referenced_columns: ['id'],
}
const targetFK = {
  name: 'links_target_fk', columns: ['target_id'], referenced_schema: 'public',
  referenced_table: 'targets', referenced_columns: ['id'],
}
const parent: ResourceMetadata = {
  name: 'parents', kind: 'table', primary_key: ['id'], foreign_keys: [],
  reverse_relations: [
    { name: parentFK.name, schema: 'public', resource: 'links', columns: ['parent_id'], referenced_columns: ['id'] },
    { name: 'notes_parent_fk', schema: 'public', resource: 'notes', columns: ['parent_id'], referenced_columns: ['id'] },
  ],
  columns: [column('id', 'bigint', { identity: 'always', editable: false }), column('name')],
}
const target: ResourceMetadata = {
  name: 'targets', kind: 'table', primary_key: ['id'], foreign_keys: [],
  columns: [column('id', 'bigint'), column('name')],
}
const junction: ResourceMetadata = {
  name: 'links', kind: 'table', primary_key: ['parent_id', 'target_id'], foreign_keys: [parentFK, targetFK],
  columns: [column('parent_id', 'bigint'), column('target_id', 'bigint')],
}
const notes: ResourceMetadata = {
  name: 'notes', kind: 'table', primary_key: ['id'], foreign_keys: [],
  columns: [column('id', 'bigint'), column('parent_id', 'bigint'), column('text')],
}
const schemas = [{ name: 'public', resources: [parent, target, junction, notes] }]
const wrappers: VueWrapper[] = []

function mountForm(mode: 'create' | 'edit', overrides: Partial<ResourceMetadata> = {}) {
  const parentResource = { ...parent, ...overrides }
  const currentSchemas = [{ name: 'public', resources: [parentResource, target, junction, notes] }]
  const fetchMock = vi.fn().mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    if (url.includes('/links/records?')) {
      return new Response(JSON.stringify({ error: { code: 'RECORDS_QUERY_FAILED', message: 'Failed' } }), { status: 500 })
    }
    if (url.includes('/links/relations/resolve')) {
      return new Response(JSON.stringify({ relations: [{ name: targetFK.name, records: [] }] }), { status: 200 })
    }
    if (url.includes('/notes/records?')) {
      return new Response(JSON.stringify({ records: [{ id: '8', parent_id: '1', text: 'Ordinary child' }], page: 1, limit: 5, has_next: false }), { status: 200 })
    }
    if (url.includes('/targets/records?')) {
      return new Response(JSON.stringify({ records: [], page: 1, limit: 20, has_next: false }), { status: 200 })
    }
    throw new Error(`Unexpected ${init?.method ?? 'GET'} ${url}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const i18n = createVelmirI18n()
  const wrapper = mount(RecordForm, {
    attachTo: document.body,
    props: {
      backendBaseURL: '', schemaName: 'public', resource: parentResource, schemas: currentSchemas,
      mode, record: mode === 'edit' ? { id: '1', name: 'Saved parent' } : undefined,
      updateAllowed: false, writeEnabled: true,
    },
    global: { plugins: [i18n] },
  })
  wrappers.push(wrapper)
  return { wrapper, fetchMock, i18n }
}

beforeEach(() => {
  authState.status = 'authenticated'
  authState.permissions = [{ schema: '*', resource: '*', operations: ['read', 'create', 'delete'] }]
})

afterEach(() => {
  while (wrappers.length) wrappers.pop()?.unmount()
  vi.unstubAllGlobals()
  handleUnauthorized()
  document.body.innerHTML = ''
})

describe('RecordForm many-to-many integration', () => {
  it('does not query any relation for an unsaved parent and preserves parent values across locale changes', async () => {
    const { wrapper, fetchMock, i18n } = mountForm('create')
    await flushPromises()
    expect(wrapper.text()).toContain('Сначала сохраните запись')
    expect(wrapper.find('.many-to-many-panel').exists()).toBe(false)
    expect(fetchMock).not.toHaveBeenCalled()
    await wrapper.get('#field-name').setValue('Unsaved parent')
    i18n.global.locale.value = 'en'
    await nextTick()
    expect((wrapper.get('#field-name').element as HTMLInputElement).value).toBe('Unsaved parent')
    expect(wrapper.text()).toContain('Save the record first')
  })

  it('replaces only the supported junction reverse panel and isolates its error from the parent and ordinary child panel', async () => {
    const { wrapper } = mountForm('edit')
    await flushPromises()
    expect(wrapper.findAll('.many-to-many-panel')).toHaveLength(1)
    expect(wrapper.findAll('.reverse-relation-panel:not(.many-to-many-panel)')).toHaveLength(1)
    expect(wrapper.text()).toContain('Ordinary child')
    expect(wrapper.text()).toContain('Не удалось загрузить связанные записи')
    expect(wrapper.find('form.record-form').exists()).toBe(true)
    expect(wrapper.find('button[type="submit"]').exists()).toBe(false)
    await wrapper.get('#field-name').setValue('Parent remains editable in state')
    expect((wrapper.get('#field-name').element as HTMLInputElement).value).toBe('Parent remains editable in state')
  })

  it('keeps an unsupported junction as an ordinary reverse panel without a many-to-many editor', async () => {
    const unsupported = {
      ...junction,
      columns: [...junction.columns, column('required_attribute')],
    }
    const unsupportedSchemas = [{ name: 'public', resources: [parent, target, unsupported, notes] }]
    authState.permissions = [{ schema: '*', resource: '*', operations: ['read'] }]
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ records: [], page: 1, limit: 5, has_next: false }), { status: 200 })))
    const wrapper = mount(RecordForm, {
      props: {
        backendBaseURL: '', schemaName: 'public', resource: parent, schemas: unsupportedSchemas,
        mode: 'edit', record: { id: '1', name: 'Parent' }, updateAllowed: false, writeEnabled: true,
      },
      global: { plugins: [createVelmirI18n()] },
    })
    wrappers.push(wrapper)
    await flushPromises()
    expect(wrapper.find('.many-to-many-panel').exists()).toBe(false)
    expect(wrapper.findAll('.reverse-relation-panel')).toHaveLength(2)
    expect(wrapper.find('.many-to-many-add').exists()).toBe(false)
  })

  it('hides the many-to-many panel and sends no requests when junction read is absent', async () => {
    authState.permissions = [
      { schema: 'public', resource: 'parents', operations: ['read'] },
      { schema: 'public', resource: 'targets', operations: ['read'] },
      { schema: 'public', resource: 'notes', operations: ['read'] },
    ]
    const filteredParent: ResourceMetadata = {
      ...parent,
      reverse_relations: parent.reverse_relations?.filter((relation) => relation.resource === 'notes'),
    }
    const filteredSchemas = [{ name: 'public', resources: [filteredParent, target, notes] }]
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ records: [], page: 1, limit: 5, has_next: false }), { status: 200 })))
    const wrapper = mount(RecordForm, {
      props: {
        backendBaseURL: '', schemaName: 'public', resource: filteredParent, schemas: filteredSchemas,
        mode: 'edit', record: { id: '1', name: 'Parent' }, updateAllowed: false, writeEnabled: true,
      },
      global: { plugins: [createVelmirI18n()] },
    })
    wrappers.push(wrapper)
    await flushPromises()
    expect(wrapper.find('.many-to-many-panel').exists()).toBe(false)
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(String(vi.mocked(fetch).mock.calls[0][0])).toContain('/notes/records?')
  })
})
