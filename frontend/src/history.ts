import { handleUnauthorized } from './auth'
import type { RecordRow } from './records'
import type { ColumnMetadata, ResourceMetadata } from './schema'

export const DEFAULT_HISTORY_LIMIT = 20
export const MAX_HISTORY_LIMIT = 100

export type HistoryAction = 'create' | 'update' | 'delete'

export interface HistoryActor {
  id: number
  email: string
  display_name: string
}

export interface HistoryEvent {
  id: string
  actor: HistoryActor
  timestamp: string
  action: HistoryAction
  schema: string
  resource: string
  record_key: RecordRow
  before: RecordRow | null
  after: RecordRow | null
}

export interface HistoryResponse {
  events: HistoryEvent[]
  page: number
  limit: number
  has_next: boolean
}

export interface HistoryChange {
  column: ColumnMetadata
  before: unknown
  after: unknown
}

export class HistoryRequestError extends Error {
  constructor(readonly code: string, message: string, readonly status: number) {
    super(message)
    this.name = 'HistoryRequestError'
  }
}

export function historyChanges(event: HistoryEvent, resource: ResourceMetadata): HistoryChange[] {
  const changes: HistoryChange[] = []
  for (const column of resource.columns) {
    const hasBefore = event.before !== null && Object.hasOwn(event.before, column.name)
    const hasAfter = event.after !== null && Object.hasOwn(event.after, column.name)
    if (event.action === 'create' && hasAfter) {
      changes.push({ column, before: undefined, after: event.after![column.name] })
    } else if (event.action === 'delete' && hasBefore) {
      changes.push({ column, before: event.before![column.name], after: undefined })
    } else if (event.action === 'update' && (hasBefore || hasAfter) && !valuesEqual(event.before?.[column.name], event.after?.[column.name])) {
      changes.push({ column, before: event.before?.[column.name], after: event.after?.[column.name] })
    }
  }
  return changes
}

export async function fetchRecordHistory(
  backendBaseURL: string,
  schema: string,
  resource: string,
  key: string,
  page = 1,
  limit = DEFAULT_HISTORY_LIMIT,
  signal?: AbortSignal,
): Promise<HistoryResponse> {
  const query = new URLSearchParams({ page: String(page), limit: String(limit) })
  const response = await fetch(
    `${backendBaseURL}/api/resources/${encodeURIComponent(schema)}/${encodeURIComponent(resource)}/records/${encodeURIComponent(key)}/history?${query}`,
    { headers: { Accept: 'application/json' }, credentials: 'same-origin', signal },
  )
  const body: unknown = await response.json()
  if (response.status === 401) handleUnauthorized()
  if (!response.ok) {
    const error = readError(body)
    throw new HistoryRequestError(error.code, error.message, response.status)
  }
  if (!isHistoryResponse(body)) throw new Error('Invalid history response')
  return body
}

function valuesEqual(left: unknown, right: unknown): boolean {
  return JSON.stringify(sortValue(left)) === JSON.stringify(sortValue(right))
}

function sortValue(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(sortValue)
  if (isRecord(value)) {
    return Object.fromEntries(Object.keys(value).sort().map((key) => [key, sortValue(value[key])]))
  }
  return value
}

function isHistoryResponse(value: unknown): value is HistoryResponse {
  if (!isRecord(value) || !Array.isArray(value.events)) return false
  return (
    value.events.every(isHistoryEvent) &&
    typeof value.page === 'number' && Number.isSafeInteger(value.page) && value.page > 0 &&
    typeof value.limit === 'number' && Number.isInteger(value.limit) && value.limit >= 1 && value.limit <= MAX_HISTORY_LIMIT &&
    typeof value.has_next === 'boolean'
  )
}

function isHistoryEvent(value: unknown): value is HistoryEvent {
  if (!isRecord(value) || !isRecord(value.actor) || !isRecord(value.record_key)) return false
  return (
    typeof value.id === 'string' && value.id !== '' &&
    typeof value.actor.id === 'number' && Number.isSafeInteger(value.actor.id) &&
    typeof value.actor.email === 'string' && typeof value.actor.display_name === 'string' &&
    typeof value.timestamp === 'string' &&
    (value.action === 'create' || value.action === 'update' || value.action === 'delete') &&
    typeof value.schema === 'string' && typeof value.resource === 'string' &&
    (value.before === null || isRecord(value.before)) &&
    (value.after === null || isRecord(value.after))
  )
}

function readError(value: unknown): { code: string; message: string } {
  if (isRecord(value) && isRecord(value.error)) {
    return {
      code: typeof value.error.code === 'string' ? value.error.code : 'HISTORY_QUERY_FAILED',
      message: typeof value.error.message === 'string' ? value.error.message : 'Could not load record history',
    }
  }
  return { code: 'HISTORY_QUERY_FAILED', message: 'Could not load record history' }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
