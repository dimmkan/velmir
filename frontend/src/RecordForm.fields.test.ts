// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import RecordForm from './RecordForm.vue'
import { authState } from './auth'
import { createVelmirI18n } from './i18n'
import { buildUpdatePayload, initialFormState, validateRecordForm } from './recordForm'
import type { ColumnMetadata, ResourceMetadata } from './schema'

function column(name: string, overrides: Partial<ColumnMetadata> = {}): ColumnMetadata {
  return {
    name, data_type: name.includes('id') ? 'bigint' : 'text', nullable: false, default: null,
    primary_key: false, identity: '', generated: false, editable: true, enum_values: [], ...overrides,
  }
}

const resource: ResourceMetadata = {
  name: 'books', kind: 'table', primary_key: ['id'],
  foreign_keys: [{
    name: 'books_author_fk', columns: ['author_id'], referenced_schema: 'public',
    referenced_table: 'authors', referenced_columns: ['id'],
  }],
  creatable_fields: ['author_id', 'title'], updatable_fields: ['title'],
  can_create: true, can_update: true,
  columns: [
    column('id', { primary_key: true, identity: 'always', editable: false, default: 'nextval' }),
    column('author_id'), column('title'), column('note', { nullable: true }),
  ],
}

const authors: ResourceMetadata = {
  name: 'authors', kind: 'table', primary_key: ['id'], foreign_keys: [],
  columns: [column('id', { primary_key: true }), column('name')],
}

afterEach(() => {
  vi.unstubAllGlobals()
  authState.permissions = []
})

describe('RecordForm effective field permissions', () => {
  it('omits stale unauthorized values and validates only accessible required fields', () => {
    const state = initialFormState('create', resource)
    state.author_id.value = '1'
    state.title.value = ''
    state.note.value = 'must not be sent'
    expect(validateRecordForm('create', resource, state)).toEqual({ title: 'validation.required' })

    const record = { id: '1', author_id: '1', title: 'Before', note: 'old' }
    const initial = initialFormState('edit', resource, record)
    const changed = structuredClone(initial)
    changed.title.value = 'After'
    changed.note.value = 'stale hidden value'
    changed.author_id.value = '2'
    expect(buildUpdatePayload(resource, changed, initial)).toEqual({ title: 'After' })
  })

  it('hides unauthorized controls and many-to-one selectors and submits only effective fields', async () => {
    authState.permissions = [{ schema: 'public', resource: 'authors', operations: ['read'] }]
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      record: { id: '1', author_id: '1', title: 'After', note: 'old' },
    }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    const i18n = createVelmirI18n()
    const wrapper = mount(RecordForm, {
      props: {
        backendBaseURL: '', schemaName: 'public', resource, mode: 'edit',
        record: { id: '1', author_id: '1', title: 'Before', note: 'old' },
        schemas: [{ name: 'public', resources: [resource, authors] }], updateAllowed: true, writeEnabled: true,
      },
      global: { plugins: [i18n] },
    })
    expect(wrapper.find('#field-title').exists()).toBe(true)
    expect(wrapper.find('#field-note').exists()).toBe(false)
    expect(wrapper.find('#field-author_id').exists()).toBe(false)
    expect(wrapper.find('.relation-form-field').exists()).toBe(false)
    await wrapper.get('#field-title').setValue('After')
    i18n.global.locale.value = 'en'
    await nextTick()
    expect((wrapper.get('#field-title').element as HTMLInputElement).value).toBe('After')
    await wrapper.get('form.record-form').trigger('submit.prevent')
    await flushPromises()
    const request = fetchMock.mock.calls.find((call) => call[1]?.method === 'PATCH')
    expect(JSON.parse(request![1]!.body as string)).toEqual({ values: { title: 'After' } })
  })

  it('shows impossible create and disables editing for an empty update field set', () => {
    const impossible = mount(RecordForm, {
      props: {
        backendBaseURL: '', schemaName: 'public', mode: 'create',
        resource: { ...resource, can_create: false, create_disabled_reason: 'required_field_unavailable' as const },
      },
      global: { plugins: [createVelmirI18n()] },
    })
    expect(impossible.text()).toContain('Создание записи невозможно')
    expect(impossible.find('form.record-form').exists()).toBe(false)

    const readOnly = mount(RecordForm, {
      props: {
        backendBaseURL: '', schemaName: 'public', mode: 'edit', record: { id: '1', author_id: '1', title: 'Read', note: null },
        resource: { ...resource, updatable_fields: [], can_update: false, update_disabled_reason: 'no_writable_fields' as const },
      },
      global: { plugins: [createVelmirI18n()] },
    })
    expect(readOnly.text()).toContain('Нет доступных полей для записи')
    expect(readOnly.find('button[type="submit"]').exists()).toBe(false)
    expect((readOnly.get('#field-title').element as HTMLInputElement).disabled).toBe(true)
  })
})
