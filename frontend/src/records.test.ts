import { describe, expect, it } from 'vitest'
import {
  buildRecordsQuery,
  defaultRecordsQuery,
  filterOperators,
  formatRecordValue,
  parseRecordsQuery,
  type RecordsQueryState,
} from './records'
import type { ResourceMetadata } from './schema'

const resource: ResourceMetadata = {
  name: 'items',
  kind: 'table',
  primary_key: ['id'],
  foreign_keys: [],
  columns: [
    { name: 'id', data_type: 'bigint', nullable: false, default: null, primary_key: false, identity: '', generated: false, editable: true, enum_values: [] },
    { name: 'title', data_type: 'character varying(255)', nullable: false, default: null, primary_key: false, identity: '', generated: false, editable: true, enum_values: [] },
    { name: 'payload', data_type: 'jsonb', nullable: true, default: null, primary_key: false, identity: '', generated: false, editable: true, enum_values: [] },
  ],
}

describe('records query parameters', () => {
  it('builds pagination, sorting, search and typed filters', () => {
    const state: RecordsQueryState = {
      page: 3,
      limit: 25,
      sort: 'title',
      direction: 'desc',
      search: '100% exact',
      searchColumns: ['title'],
      filters: [
        { column: 'id', operator: 'gte', value: '9223372036854775807' },
        { column: 'payload', operator: 'is_null', value: false },
      ],
    }

    const query = buildRecordsQuery(state)
    expect(query.get('page')).toBe('3')
    expect(query.get('limit')).toBe('25')
    expect(query.get('sort')).toBe('title')
    expect(query.get('direction')).toBe('desc')
    expect(query.get('search')).toBe('100% exact')
    expect(query.getAll('search_column')).toEqual(['title'])
    expect(JSON.parse(query.get('filter')!)).toEqual(state.filters)
  })

  it('restores supported state from a URL', () => {
    const original: RecordsQueryState = {
      ...defaultRecordsQuery(),
      page: 2,
      sort: 'id',
      direction: 'desc',
      search: 'guide',
      searchColumns: ['title'],
      filters: [{ column: 'title', operator: 'contains', value: 'SQL' }],
    }

    expect(parseRecordsQuery(buildRecordsQuery(original).toString(), resource)).toEqual(original)
  })

  it('drops unknown columns and invalid operators while parsing a URL', () => {
    const query = buildRecordsQuery({
      ...defaultRecordsQuery(),
      filters: [{ column: 'title', operator: 'contains', value: 'safe' }],
    })
    query.set('sort', 'id; DROP TABLE items')
    query.set('filter', JSON.stringify([{ column: 'title', operator: 'matches', value: 'x' }]))

    const parsed = parseRecordsQuery(query.toString(), resource)
    expect(parsed.sort).toBe('')
    expect(parsed.filters).toEqual([])
  })
})

describe('record values', () => {
  it('formats null, booleans, dates, JSON and large numbers without numeric coercion', () => {
    expect(formatRecordValue(null, 'text')).toBe('null')
    expect(formatRecordValue(false, 'boolean')).toBe('false')
    expect(formatRecordValue('2025-01-02', 'date')).toBe('2025-01-02')
    expect(formatRecordValue('2025-01-02T03:04:05Z', 'timestamp with time zone')).toBe('2025-01-02T03:04:05Z')
    expect(formatRecordValue({ big: '9223372036854775807', ok: true }, 'jsonb')).toBe('{"big":"9223372036854775807","ok":true}')
    expect(formatRecordValue('9223372036854775807', 'bigint')).toBe('9223372036854775807')
    expect(formatRecordValue('12345678901234567890.0001', 'numeric')).toBe('12345678901234567890.0001')
    expect(formatRecordValue('AAH/', 'bytea')).toBe('base64:AAH/')
  })
})

describe('filter metadata', () => {
  it('offers operators according to PostgreSQL types', () => {
    expect(filterOperators('character varying(255)')).toEqual(['contains', 'eq', 'neq', 'is_null'])
    expect(filterOperators('numeric(20,4)')).toEqual(['eq', 'neq', 'lt', 'lte', 'gt', 'gte', 'is_null'])
    expect(filterOperators('json')).toEqual(['is_null'])
  })
})
