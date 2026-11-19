import { getCSRFToken, handleUnauthorized } from './auth'
import type { RecordRow } from './records'
import type { ResourceMetadata } from './schema'

export type RecordValues = Record<string, unknown>

export class MutationRequestError extends Error {
  constructor(
    readonly code: string,
    message: string,
    readonly status: number,
    readonly fieldErrors: Record<string, string> = {},
    readonly mutationCommitted = false,
    readonly record: RecordRow | null = null,
  ) {
    super(message)
    this.name = 'MutationRequestError'
  }
}

export function buildRecordKey(resource: ResourceMetadata, record: RecordRow): string {
  if (resource.primary_key.length === 0) throw new Error('Record has no primary key')
  const key: RecordRow = {}
  for (const column of resource.primary_key) {
    const value = record[column]
    if (value === null || value === undefined) throw new Error(`Missing primary key value: ${column}`)
    key[column] = value
  }
  return JSON.stringify(key)
}

export async function createRecord(
  backendBaseURL: string,
  schema: string,
  resource: string,
  values: RecordValues,
  csrfToken = getCSRFToken(),
): Promise<RecordRow> {
  return sendMutation(backendBaseURL, schema, resource, values, undefined, csrfToken)
}

export async function updateRecord(
  backendBaseURL: string,
  schema: string,
  resource: string,
  key: string,
  values: RecordValues,
  csrfToken = getCSRFToken(),
): Promise<RecordRow> {
  return sendMutation(backendBaseURL, schema, resource, values, key, csrfToken)
}

async function sendMutation(
  backendBaseURL: string,
  schema: string,
  resource: string,
  values: RecordValues,
  key: string | undefined,
  csrfToken: string,
): Promise<RecordRow> {
  const path = `${backendBaseURL}/api/resources/${encodeURIComponent(schema)}/${encodeURIComponent(resource)}/records`
  const url = key === undefined ? path : `${path}/${encodeURIComponent(key)}`
  const response = await fetch(url, {
    method: key === undefined ? 'POST' : 'PATCH',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
    credentials: 'same-origin',
    body: JSON.stringify({ values }),
  })
  const body: unknown = await response.json()
  if (response.status === 401) handleUnauthorized()
  if (!response.ok) throw readMutationError(body, response.status)
  if (!isRecord(body) || !isRecord(body.record)) throw new Error('Invalid mutation response')
  return body.record
}

function readMutationError(body: unknown, status: number): MutationRequestError {
  if (isRecord(body) && isRecord(body.error)) {
    const fieldErrors: Record<string, string> = {}
    if (isRecord(body.error.field_errors)) {
      for (const [field, message] of Object.entries(body.error.field_errors)) {
        if (typeof message === 'string') fieldErrors[field] = message
      }
    }
    return new MutationRequestError(
      typeof body.error.code === 'string' ? body.error.code : 'RECORD_MUTATION_FAILED',
      typeof body.error.message === 'string' ? body.error.message : 'Could not save the record',
      status,
      fieldErrors,
      body.error.mutation_committed === true,
      isRecord(body.record) ? body.record : null,
    )
  }
  return new MutationRequestError('RECORD_MUTATION_FAILED', 'Could not save the record', status)
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
