import type { RecordRow } from './records'
import type { ResourceMetadata } from './schema'

export type RecordValues = Record<string, unknown>

export class MutationRequestError extends Error {
  constructor(
    readonly code: string,
    message: string,
    readonly fieldErrors: Record<string, string> = {},
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
): Promise<RecordRow> {
  return sendMutation(backendBaseURL, schema, resource, values)
}

export async function updateRecord(
  backendBaseURL: string,
  schema: string,
  resource: string,
  key: string,
  values: RecordValues,
): Promise<RecordRow> {
  return sendMutation(backendBaseURL, schema, resource, values, key)
}

async function sendMutation(
  backendBaseURL: string,
  schema: string,
  resource: string,
  values: RecordValues,
  key?: string,
): Promise<RecordRow> {
  const path = `${backendBaseURL}/api/resources/${encodeURIComponent(schema)}/${encodeURIComponent(resource)}/records`
  const url = key === undefined ? path : `${path}/${encodeURIComponent(key)}`
  const response = await fetch(url, {
    method: key === undefined ? 'POST' : 'PATCH',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    body: JSON.stringify({ values }),
  })
  const body: unknown = await response.json()
  if (!response.ok) throw readMutationError(body)
  if (!isRecord(body) || !isRecord(body.record)) throw new Error('Invalid mutation response')
  return body.record
}

function readMutationError(body: unknown): MutationRequestError {
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
      fieldErrors,
    )
  }
  return new MutationRequestError('RECORD_MUTATION_FAILED', 'Could not save the record')
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
