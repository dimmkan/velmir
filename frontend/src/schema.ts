export interface SchemaResponse {
  schemas: SchemaMetadata[]
}

export interface SchemaMetadata {
  name: string
  resources: ResourceMetadata[]
}

export interface ResourceMetadata {
  name: string
  kind: 'table' | 'view'
  columns: ColumnMetadata[]
  primary_key: string[]
  foreign_keys: ForeignKeyMetadata[]
}

export interface ColumnMetadata {
  name: string
  data_type: string
  nullable: boolean
  default: string | null
}

export interface ForeignKeyMetadata {
  name: string
  columns: string[]
  referenced_schema: string
  referenced_table: string
  referenced_columns: string[]
}

export class SchemaRequestError extends Error {
  constructor(readonly code: string) {
    super(code)
    this.name = 'SchemaRequestError'
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

function isStringArray(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((item) => typeof item === 'string')
}

function isColumn(value: unknown): value is ColumnMetadata {
  if (!isRecord(value)) return false
  return (
    typeof value.name === 'string' &&
    typeof value.data_type === 'string' &&
    typeof value.nullable === 'boolean' &&
    (typeof value.default === 'string' || value.default === null)
  )
}

function isForeignKey(value: unknown): value is ForeignKeyMetadata {
  if (!isRecord(value)) return false
  return (
    typeof value.name === 'string' &&
    isStringArray(value.columns) &&
    typeof value.referenced_schema === 'string' &&
    typeof value.referenced_table === 'string' &&
    isStringArray(value.referenced_columns)
  )
}

function isResource(value: unknown): value is ResourceMetadata {
  if (!isRecord(value)) return false
  return (
    typeof value.name === 'string' &&
    (value.kind === 'table' || value.kind === 'view') &&
    Array.isArray(value.columns) &&
    value.columns.every(isColumn) &&
    isStringArray(value.primary_key) &&
    Array.isArray(value.foreign_keys) &&
    value.foreign_keys.every(isForeignKey)
  )
}

function isSchemaResponse(value: unknown): value is SchemaResponse {
  if (!isRecord(value) || !Array.isArray(value.schemas)) return false
  return value.schemas.every(
    (schema) =>
      isRecord(schema) &&
      typeof schema.name === 'string' &&
      Array.isArray(schema.resources) &&
      schema.resources.every(isResource),
  )
}

function errorCode(value: unknown) {
  if (!isRecord(value) || !isRecord(value.error) || typeof value.error.code !== 'string') {
    return 'SCHEMA_REQUEST_FAILED'
  }
  return value.error.code
}

export async function fetchSchema(backendBaseURL: string, signal?: AbortSignal) {
  const response = await fetch(`${backendBaseURL}/api/schema`, {
    headers: { Accept: 'application/json' },
    signal,
  })
  const body: unknown = await response.json()

  if (!response.ok) {
    throw new SchemaRequestError(errorCode(body))
  }
  if (!isSchemaResponse(body)) {
    throw new Error('Invalid schema response')
  }
  return body
}
