import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { authState, handleUnauthorized } from './auth'
import {
  buildRelatedRelationPlans,
  chunkRelatedRelationPlans,
  fetchRelatedLabels,
  MAX_RESOLVE_KEYS,
  readableReverseRelations,
  RelatedRecordsRequestError,
  reverseRelationParentFilters,
} from './relatedRecords'
import type { ColumnMetadata, ResourceMetadata } from './schema'

function column(name: string, dataType = 'text'): ColumnMetadata {
  return {
    name, data_type: dataType, nullable: true, default: null, primary_key: false,
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
const schemas = [{ name: 'public', resources: [books, authors] }]

beforeEach(() => {
  authState.status = 'authenticated'
  authState.permissions = [{ schema: '*', resource: '*', operations: ['read'] }]
})

afterEach(() => {
  vi.unstubAllGlobals()
  handleUnauthorized()
})

describe('related label batching', () => {
  it('deduplicates repeated FK values and skips null before one HTTP request', async () => {
    const plans = buildRelatedRelationPlans(books, [
      { id: '1', author_id: '9223372036854775807', title: 'One' },
      { id: '2', author_id: '9223372036854775807', title: 'Two' },
      { id: '3', author_id: null, title: 'No author' },
    ], schemas)
    expect(plans[0].keys).toEqual([{ id: '9223372036854775807' }])
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      relations: [{ name: 'books_author_fk', records: [{ id: '9223372036854775807', name: 'Author' }] }],
    }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    const labels = await fetchRelatedLabels('', 'public', 'books', plans)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toContain('/api/resources/public/books/relations/resolve')
    expect(init.method).toBe('POST')
    expect(init.headers).not.toHaveProperty('X-CSRF-Token')
    expect(JSON.parse(init.body)).toEqual({
      relations: [{ name: 'books_author_fk', keys: [{ id: '9223372036854775807' }] }],
    })
    const option = labels.get('books_author_fk')?.get('["9223372036854775807"]')
    expect(option?.label).toBe('Author')
    expect(option?.keyLabel).toBe('id=9223372036854775807')
  })

  it('splits only server-limit overflows, never rows or individual keys', () => {
    const plan = buildRelatedRelationPlans(books, [], schemas)[0]
    plan.keys = Array.from({ length: MAX_RESOLVE_KEYS + 1 }, (_, index) => ({ id: String(index + 1) }))
    const batches = chunkRelatedRelationPlans([plan])
    expect(batches.map((batch) => batch[0].keys.length)).toEqual([MAX_RESOLVE_KEYS, 1])
  })

  it('keeps 403 authenticated and rejects without exposing labels', async () => {
    const plans = buildRelatedRelationPlans(books, [{ author_id: '1' }], schemas)
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      error: { code: 'FORBIDDEN', message: 'Denied' },
    }), { status: 403 })))
    const request = fetchRelatedLabels('', 'public', 'books', plans)
    await expect(request).rejects.toEqual(expect.objectContaining<Partial<RelatedRecordsRequestError>>({ status: 403, code: 'FORBIDDEN' }))
    expect(authState.status).toBe('authenticated')
  })

  it('makes no related request when referenced read is absent', async () => {
    authState.permissions = [{ schema: 'public', resource: 'books', operations: ['read'] }]
    const plans = buildRelatedRelationPlans(books, [{ author_id: '1' }], schemas)
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    await expect(fetchRelatedLabels('', 'public', 'books', plans)).resolves.toEqual(new Map())
    expect(plans).toEqual([])
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

describe('reverse relation filters and permissions', () => {
  const parent: ResourceMetadata = {
    name: 'parents', kind: 'table', primary_key: ['tenant_id', 'external_id'], foreign_keys: [],
    reverse_relations: [{
      name: 'children_parent_fk', schema: 'public', resource: 'children',
      columns: ['tenant_id', 'parent_id'], referenced_columns: ['tenant_id', 'external_id'],
    }],
    columns: [column('tenant_id', 'bigint'), column('external_id', 'uuid'), column('name')],
  }
  const child: ResourceMetadata = {
    name: 'children', kind: 'table', primary_key: ['id'], foreign_keys: [],
    columns: [column('id', 'bigint'), column('tenant_id', 'bigint'), column('parent_id', 'uuid'), column('title')],
  }
  const relation = parent.reverse_relations![0]

  it('builds typed filters for every simple/composite parent key part without number coercion', () => {
    expect(reverseRelationParentFilters(relation, {
      tenant_id: '9223372036854775807', external_id: '00112233-4455-6677-8899-aabbccddeeff',
    })).toEqual([
      { column: 'tenant_id', operator: 'eq', value: '9223372036854775807' },
      { column: 'parent_id', operator: 'eq', value: '00112233-4455-6677-8899-aabbccddeeff' },
    ])
    expect(reverseRelationParentFilters(relation, { tenant_id: '1', external_id: null })).toBeNull()
    expect(reverseRelationParentFilters(relation, undefined)).toBeNull()
  })

  it('hides panels and metadata for child resources without read', () => {
    expect(readableReverseRelations(parent, [{ name: 'public', resources: [parent, child] }])).toHaveLength(1)
    authState.permissions = [{ schema: 'public', resource: 'parents', operations: ['read'] }]
    expect(readableReverseRelations(parent, [{ name: 'public', resources: [parent] }])).toEqual([])
  })
})
