import { handleUnauthorized } from './auth'
import { readableColumns, type ColumnMetadata, type ResourceMetadata } from './schema'

export const DEFAULT_RECORDS_LIMIT = 50
export const MAX_RECORDS_LIMIT = 100

export type SortDirection = 'asc' | 'desc'
export type FilterOperator = 'eq' | 'neq' | 'contains' | 'lt' | 'lte' | 'gt' | 'gte' | 'is_null'

export interface RecordFilter {
  column: string
  operator: FilterOperator
  value: string | boolean
}

export interface RecordsQueryState {
  page: number
  limit: number
  sort: string
  direction: SortDirection
  search: string
  searchColumns: string[]
  filters: RecordFilter[]
}

export type RecordRow = Record<string, unknown>

export interface RecordsResponse {
  records: RecordRow[]
  page: number
  limit: number
  has_next: boolean
}

export class RecordsRequestError extends Error {
  constructor(readonly code: string, message: string, readonly status: number) {
    super(message)
    this.name = 'RecordsRequestError'
  }
}

export function defaultRecordsQuery(): RecordsQueryState {
  return {
    page: 1,
    limit: DEFAULT_RECORDS_LIMIT,
    sort: '',
    direction: 'asc',
    search: '',
    searchColumns: [],
    filters: [],
  }
}

export function buildRecordsQuery(state: RecordsQueryState): URLSearchParams {
  const query = new URLSearchParams()
  query.set('page', String(state.page))
  query.set('limit', String(state.limit))
  if (state.sort) {
    query.set('sort', state.sort)
    query.set('direction', state.direction)
  }
  if (state.search) {
    query.set('search', state.search)
    for (const column of state.searchColumns) query.append('search_column', column)
  }
  if (state.filters.length) query.set('filter', JSON.stringify(state.filters))
  return query
}

export function parseRecordsQuery(search: string, resource: ResourceMetadata): RecordsQueryState {
  const query = new URLSearchParams(search)
  const state = defaultRecordsQuery()
  const page = Number(query.get('page'))
  if (Number.isSafeInteger(page) && page > 0) state.page = page
  const limit = Number(query.get('limit'))
  if (Number.isInteger(limit) && limit >= 1 && limit <= MAX_RECORDS_LIMIT) state.limit = limit

  const sort = query.get('sort') ?? ''
  const columns = readableColumns(resource)
  const column = columns.find((item) => item.name === sort)
  if (column && isSortableColumn(column)) state.sort = sort
  if (query.get('direction') === 'desc') state.direction = 'desc'

  state.search = query.get('search') ?? ''
  const searchable = new Set(readableColumns(resource).filter(isTextColumn).map((item) => item.name))
  state.searchColumns = [...new Set(query.getAll('search_column').filter((name) => searchable.has(name)))]
  state.filters = parseFilters(query.get('filter'), resource)
  return state
}

export function isTextColumn(column: ColumnMetadata): boolean {
  return ['text', 'character varying', 'character', 'citext', 'name'].includes(baseType(column.data_type))
}

export function isSortableColumn(column: ColumnMetadata): boolean {
  return isTextColumn(column) || [
    'boolean',
    'smallint',
    'integer',
    'bigint',
    'real',
    'double precision',
    'numeric',
    'decimal',
    'date',
    'timestamp without time zone',
    'timestamp with time zone',
    'uuid',
    'bytea',
    'jsonb',
  ].includes(baseType(column.data_type))
}

export function filterOperators(dataType: string): FilterOperator[] {
  const type = baseType(dataType)
  if (isTextType(type)) return ['contains', 'eq', 'neq', 'is_null']
  if (type === 'boolean') return ['eq', 'neq', 'is_null']
  if (['smallint', 'integer', 'bigint', 'real', 'double precision', 'numeric', 'decimal', 'date', 'timestamp without time zone', 'timestamp with time zone', 'uuid'].includes(type)) {
    return ['eq', 'neq', 'lt', 'lte', 'gt', 'gte', 'is_null']
  }
  return ['is_null']
}

export function formatRecordValue(value: unknown, dataType: string, locale = 'ru'): string {
  if (value === null) return 'null'
  if (typeof value === 'boolean') return value ? 'true' : 'false'
  if (typeof value === 'object') return JSON.stringify(value)
  if (typeof value === 'string') {
    const type = baseType(dataType)
    if (type === 'bytea') return `base64:${value}`
    if (type === 'date' || type === 'timestamp without time zone' || type === 'timestamp with time zone') {
      return formatTemporalValue(value, type, locale)
    }
    return value
  }
  return String(value)
}

function formatTemporalValue(
  value: string,
  dataType: 'date' | 'timestamp without time zone' | 'timestamp with time zone',
  locale: string,
): string {
  const date = new Date(dataType === 'date' ? `${value}T00:00:00Z` : value)
  if (Number.isNaN(date.getTime())) return value
  const options: Intl.DateTimeFormatOptions = dataType === 'date'
    ? { dateStyle: 'medium', timeZone: 'UTC' }
    : {
        dateStyle: 'medium',
        timeStyle: 'medium',
        ...(dataType === 'timestamp without time zone' ? { timeZone: 'UTC' } : {}),
      }
  try {
    return new Intl.DateTimeFormat(locale, options).format(date)
  } catch {
    return value
  }
}

export async function fetchRecords(
  backendBaseURL: string,
  schema: string,
  resource: string,
  state: RecordsQueryState,
  signal?: AbortSignal,
): Promise<RecordsResponse> {
  const query = buildRecordsQuery(state)
  const response = await fetch(
    `${backendBaseURL}/api/resources/${encodeURIComponent(schema)}/${encodeURIComponent(resource)}/records?${query}`,
    { headers: { Accept: 'application/json' }, credentials: 'same-origin', signal },
  )
  const body: unknown = await response.json()
  if (response.status === 401) handleUnauthorized()
  if (!response.ok) {
    const error = readError(body)
    throw new RecordsRequestError(error.code, error.message, response.status)
  }
  if (!isRecordsResponse(body)) throw new Error('Invalid records response')
  return body
}

function baseType(dataType: string): string {
  return dataType.toLowerCase().trim().replace(/\([^)]*\)/g, '').trim()
}

function isTextType(dataType: string): boolean {
  return ['text', 'character varying', 'character', 'citext', 'name'].includes(dataType)
}

function parseFilters(raw: string | null, resource: ResourceMetadata): RecordFilter[] {
  if (!raw) return []
  try {
    const value: unknown = JSON.parse(raw)
    if (!Array.isArray(value)) return []
    return value.filter((filter): filter is RecordFilter => {
      if (!isRecord(filter) || typeof filter.column !== 'string' || typeof filter.operator !== 'string') return false
      const column = readableColumns(resource).find((item) => item.name === filter.column)
      if (!column || !filterOperators(column.data_type).includes(filter.operator as FilterOperator)) return false
      if (filter.operator === 'is_null') return typeof filter.value === 'boolean'
      return typeof filter.value === 'string'
    })
  } catch {
    return []
  }
}

function isRecordsResponse(value: unknown): value is RecordsResponse {
  if (!isRecord(value) || !Array.isArray(value.records)) return false
  return (
    value.records.every((record) => isRecord(record)) &&
    typeof value.page === 'number' &&
    Number.isSafeInteger(value.page) &&
    value.page > 0 &&
    typeof value.limit === 'number' &&
    Number.isInteger(value.limit) &&
    value.limit >= 1 &&
    value.limit <= MAX_RECORDS_LIMIT &&
    typeof value.has_next === 'boolean'
  )
}

function readError(value: unknown): { code: string; message: string } {
  if (isRecord(value) && isRecord(value.error)) {
    return {
      code: typeof value.error.code === 'string' ? value.error.code : 'RECORDS_REQUEST_FAILED',
      message: typeof value.error.message === 'string' ? value.error.message : 'Could not load records',
    }
  }
  return { code: 'RECORDS_REQUEST_FAILED', message: 'Could not load records' }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
