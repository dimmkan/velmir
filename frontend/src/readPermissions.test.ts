// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import RecordForm from './RecordForm.vue'
import { authState } from './auth'
import { createVelmirI18n } from './i18n'
import { historyChanges, type HistoryEvent } from './history'
import { detectManyToManyRelation } from './manyToMany'
import { buildUpdatePayload, initialFormState, validateRecordForm } from './recordForm'
import { buildRelatedRelationPlans, readableReverseRelations } from './relatedRecords'
import { isWriteOnlyColumn, readableColumns, type ColumnMetadata, type ResourceMetadata } from './schema'

function column(name: string, overrides: Partial<ColumnMetadata> = {}): ColumnMetadata {
  return {
    name, data_type: name.endsWith('id') ? 'bigint' : 'text', nullable: false, default: null,
    primary_key: false, identity: '', generated: false, editable: true, enum_values: [], ...overrides,
  }
}

const books: ResourceMetadata = {
  name: 'books', kind: 'table', primary_key: ['id'],
  columns: [column('id', { primary_key: true, editable: false }), column('title'), column('isbn'), column('author_id')],
  foreign_keys: [{ name: 'books_author_fk', columns: ['author_id'], referenced_schema: 'public', referenced_table: 'authors', referenced_columns: ['id'] }],
  reverse_relations: [], readable_fields: ['id', 'title'], write_only_fields: ['isbn'],
  creatable_fields: ['title', 'isbn', 'author_id'], updatable_fields: ['title', 'isbn'], can_create: true, can_update: true,
}
const authors: ResourceMetadata = {
  name: 'authors', kind: 'table', primary_key: ['id'], foreign_keys: [],
  columns: [column('id', { primary_key: true }), column('name')], readable_fields: ['id', 'name'], write_only_fields: [],
  creatable_fields: [], updatable_fields: [],
}
const schemas = [{ name: 'public', resources: [books, authors] }]

beforeEach(() => {
  authState.status = 'authenticated'
  authState.permissions = [{ schema: '*', resource: '*', operations: ['read'] }]
})
afterEach(() => {
  authState.permissions = []
  vi.unstubAllGlobals()
})

describe('8B effective readable metadata', () => {
  it('supports PK-only tables, hidden URL controls and write-only field distinction', () => {
    const pkOnly = { ...books, readable_fields: ['id'], write_only_fields: [] }
    expect(readableColumns(pkOnly).map((item) => item.name)).toEqual(['id'])
    expect(isWriteOnlyColumn(books, books.columns[2])).toBe(true)
    expect(isWriteOnlyColumn(books, books.columns[1])).toBe(false)
  })

  it('does not submit or validate an untouched write-only current value, but submits an explicit replacement', () => {
    const record = { id: '1', title: 'Visible' }
    const initial = initialFormState('edit', books, record)
    expect(initial.isbn.value).toBe('')
    expect(validateRecordForm('edit', books, initial, initial)).toEqual({})
    expect(buildUpdatePayload(books, initial, initial)).toEqual({})
    const changed = structuredClone(initial)
    changed.isbn.value = 'new-write-only-value'
    expect(buildUpdatePayload(books, changed, initial)).toEqual({ isbn: 'new-write-only-value' })
  })

  it('shows a localized hidden state rather than null, undefined or a stale value', async () => {
    const i18n = createVelmirI18n()
    const wrapper = mount(RecordForm, {
      props: { backendBaseURL: '', schemaName: 'public', resource: books, mode: 'edit', record: { id: '1', title: 'Visible' }, updateAllowed: true },
      global: { plugins: [i18n] },
    })
    expect(wrapper.text()).toContain('Текущее значение скрыто')
    expect(wrapper.text()).not.toContain('undefined')
    expect(wrapper.find('#field-isbn').exists()).toBe(true)
    expect(wrapper.find('#field-author_id').exists()).toBe(false)
    i18n.global.locale.value = 'en'
    await flushPromises()
    expect(wrapper.text()).toContain('The current value is hidden')
  })

  it('renders only fields actually present in filtered history', () => {
    const event: HistoryEvent = {
      id: '1', actor: { id: 1, email: 'a@example.test', display_name: 'A' }, timestamp: '2026-01-01T00:00:00Z',
      action: 'update', schema: 'public', resource: 'books', record_key: { id: '1' },
      before: { id: '1', title: 'Before' }, after: { id: '1', title: 'After' },
    }
    expect(historyChanges(event, books).map((change) => change.column.name)).toEqual(['title'])
    expect(historyChanges(event, { ...books, readable_fields: ['id'] })).toEqual([])
  })

  it('does not plan labels or reverse requests when local/referenced fields are hidden', () => {
    expect(buildRelatedRelationPlans(books, [{ id: '1', author_id: '2' }], schemas)).toEqual([])
    const hiddenTarget = { ...authors, readable_fields: [] }
    expect(buildRelatedRelationPlans(
      { ...books, readable_fields: ['id', 'author_id'] },
      [{ id: '1', author_id: '2' }],
      [{ name: 'public', resources: [books, hiddenTarget] }],
    )).toEqual([])

    const parent: ResourceMetadata = {
      ...authors,
      reverse_relations: [{ name: 'books_author_fk', schema: 'public', resource: 'books', columns: ['author_id'], referenced_columns: ['id'] }],
    }
    expect(readableReverseRelations(parent, schemas)).toEqual([])
    expect(readableReverseRelations(parent, [{ name: 'public', resources: [parent, { ...books, readable_fields: ['id', 'author_id'] }] }])).toHaveLength(1)
  })

  it('rejects many-to-many detection when a junction FK is write-only', () => {
    const targets = authors
    const junction: ResourceMetadata = {
      name: 'book_authors', kind: 'table', primary_key: ['book_id', 'author_id'],
      columns: [column('book_id'), column('author_id')],
      foreign_keys: [
        { name: 'book_fk', columns: ['book_id'], referenced_schema: 'public', referenced_table: 'books', referenced_columns: ['id'] },
        { name: 'author_fk', columns: ['author_id'], referenced_schema: 'public', referenced_table: 'authors', referenced_columns: ['id'] },
      ],
      readable_fields: ['book_id'], write_only_fields: ['author_id'], creatable_fields: ['book_id', 'author_id'], updatable_fields: [],
    }
    expect(detectManyToManyRelation('public', books, 'public', junction, [
      { name: 'public', resources: [books, targets, junction] },
    ])).toBeNull()
  })
})
