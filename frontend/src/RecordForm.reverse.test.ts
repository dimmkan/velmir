// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import RecordForm from './RecordForm.vue'
import { authState, handleUnauthorized } from './auth'
import { createVelmirI18n } from './i18n'
import type { ColumnMetadata, ResourceMetadata, ReverseRelationMetadata } from './schema'

function column(name: string, dataType = 'text'): ColumnMetadata {
  return {
    name, data_type: dataType, nullable: false, default: null, primary_key: name === 'id',
    identity: name === 'id' ? 'always' : '', generated: false, editable: name !== 'id', enum_values: [],
  }
}

const childRelation: ReverseRelationMetadata = {
  name: 'children_parent_fk', schema: 'public', resource: 'children', columns: ['parent_id'], referenced_columns: ['id'],
}
const noteRelation: ReverseRelationMetadata = {
  name: 'notes_parent_fk', schema: 'public', resource: 'notes', columns: ['parent_id'], referenced_columns: ['id'],
}
const parent: ResourceMetadata = {
  name: 'parents', kind: 'table', primary_key: ['id'], foreign_keys: [],
  reverse_relations: [childRelation, noteRelation],
  columns: [column('id', 'bigint'), column('name')],
}
const children: ResourceMetadata = {
  name: 'children', kind: 'table', primary_key: ['id'], foreign_keys: [],
  columns: [column('id', 'bigint'), column('parent_id', 'bigint'), column('title')],
}
const notes: ResourceMetadata = {
  name: 'notes', kind: 'table', primary_key: ['id'], foreign_keys: [],
  columns: [column('id', 'bigint'), column('parent_id', 'bigint'), column('text')],
}
const schemas = [{ name: 'public', resources: [parent, children, notes] }]
let wrapper: VueWrapper | null = null

function mountForm(mode: 'create' | 'edit', updateAllowed = true) {
  const fetchMock = vi.fn().mockImplementation(async (input: RequestInfo | URL) => {
    const url = String(input)
    if (url.includes('/children/')) {
      return new Response(JSON.stringify({ error: { code: 'RECORDS_QUERY_FAILED', message: 'Failed' } }), { status: 500 })
    }
    const query = new URL(url, 'http://localhost').searchParams
    return new Response(JSON.stringify({
      records: [{ id: '10', parent_id: '1', text: 'Visible note' }],
      page: Number(query.get('page')), limit: Number(query.get('limit')), has_next: false,
    }), { status: 200 })
  })
  vi.stubGlobal('fetch', fetchMock)
  const i18n = createVelmirI18n()
  wrapper = mount(RecordForm, {
    attachTo: document.body,
    props: {
      backendBaseURL: '', schemaName: 'public', resource: parent, schemas, mode, updateAllowed,
      record: mode === 'edit' ? { id: '1', name: 'Saved parent' } : undefined,
    },
    global: { plugins: [i18n] },
  })
  return { wrapper, fetchMock, i18n }
}

beforeEach(() => {
  authState.status = 'authenticated'
  authState.permissions = [{ schema: '*', resource: '*', operations: ['read', 'create', 'update'] }]
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.unstubAllGlobals()
  handleUnauthorized()
  document.body.innerHTML = ''
})

describe('RecordForm reverse relation panels', () => {
  it('shows save-first for an unsaved parent and never queries children', async () => {
    const { wrapper, fetchMock, i18n } = mountForm('create')
    await flushPromises()
    expect(wrapper.text()).toContain('Сначала сохраните запись')
    expect(wrapper.findAll('.reverse-relation-panel')).toHaveLength(0)
    expect(fetchMock).not.toHaveBeenCalled()

    await wrapper.get('#field-name').setValue('Unsaved parent')
    i18n.global.locale.value = 'en'
    await nextTick()
    expect(wrapper.text()).toContain('Save the record first')
    expect((wrapper.get('#field-name').element as HTMLInputElement).value).toBe('Unsaved parent')
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('isolates one child error and preserves the other panel and unsaved parent values', async () => {
    const { wrapper } = mountForm('edit')
    await flushPromises()
    expect(wrapper.text()).toContain('Visible note')
    expect(wrapper.text()).toContain('Не удалось загрузить дочерние записи')

    await wrapper.get('#field-name').setValue('Dirty parent')
    await flushPromises()
    expect((wrapper.get('#field-name').element as HTMLInputElement).value).toBe('Dirty parent')
    expect(wrapper.text()).toContain('Visible note')
    expect(wrapper.findAll('form.record-form')).toHaveLength(1)
    expect(wrapper.findAll('.reverse-relation-panel form')).toHaveLength(0)
  })

  it('hides all panels and sends no child requests without child read', async () => {
    authState.permissions = [{ schema: 'public', resource: 'parents', operations: ['read', 'update'] }]
    const { wrapper, fetchMock } = mountForm('edit')
    await flushPromises()
    expect(wrapper.find('.reverse-relations').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Visible note')
    expect(fetchMock).not.toHaveBeenCalled()
    expect(wrapper.find('form.record-form').exists()).toBe(true)
  })

  it('opens readable child panels in a read-only parent detail without save controls', async () => {
    authState.permissions = [
      { schema: 'public', resource: 'parents', operations: ['read'] },
      { schema: 'public', resource: 'notes', operations: ['read'] },
    ]
    const { wrapper, fetchMock } = mountForm('edit', false)
    await flushPromises()
    expect(wrapper.get('.record-form-panel').attributes('aria-label')).toBe('Просмотр')
    expect((wrapper.get('#field-name').element as HTMLInputElement).value).toBe('Saved parent')
    expect(wrapper.get('#field-name').attributes('disabled')).toBeDefined()
    expect(wrapper.find('button[type="submit"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Visible note')
    expect(fetchMock.mock.calls.every(([, init]) => init?.method !== 'PATCH')).toBe(true)
  })
})
