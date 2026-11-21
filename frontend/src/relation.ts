import { isTextColumn, type RecordFilter, type RecordRow } from './records'
import { readableColumns, type ResourceMetadata, type SchemaMetadata } from './schema'

export interface RelationOption {
  record: RecordRow
  key: string
  label: string
  keyLabel: string
}

export function findReferencedResource(
  schemas: SchemaMetadata[],
  schemaName: string,
  resourceName: string,
): ResourceMetadata | null {
  return schemas
    .find((schema) => schema.name === schemaName)
    ?.resources.find((resource) => resource.name === resourceName) ?? null
}

export function relationValuesKey(values: unknown[]): string {
  return JSON.stringify(values.map((value) =>
    value !== null && value !== undefined && typeof value !== 'object' ? String(value) : value,
  ))
}

export function relationRecordKey(record: RecordRow, referencedColumns: string[]): string {
  return relationValuesKey(referencedColumns.map((column) => record[column]))
}

export function relationKeyLabel(record: RecordRow, referencedColumns: string[]): string {
  return referencedColumns
    .map((column) => `${column}=${formatKeyValue(record[column])}`)
    .join(' · ')
}

export function relationOption(
  record: RecordRow,
  resource: ResourceMetadata,
  referencedColumns: string[],
): RelationOption {
  const keyLabel = relationKeyLabel(record, referencedColumns)
  return {
    record,
    key: relationRecordKey(record, referencedColumns),
    label: displayLabel(record, resource) || keyLabel,
    keyLabel,
  }
}

export function exactRelationFilters(referencedColumns: string[], values: unknown[]): RecordFilter[] {
  return referencedColumns.map((column, index) => ({
    column,
    operator: 'eq',
    value: String(values[index]),
  }))
}

function displayLabel(record: RecordRow, resource: ResourceMetadata): string {
  const columns = readableColumns(resource)
  const prioritized = ['display_name', 'name', 'title', 'label']
  for (const name of prioritized) {
    const column = columns.find((item) => item.name === name)
    const value = readableText(record[name], column)
    if (value) return value
  }
  for (const column of columns) {
    if (prioritized.includes(column.name)) continue
    const value = readableText(record[column.name], column)
    if (value) return value
  }
  return ''
}

function readableText(value: unknown, column: ResourceMetadata['columns'][number] | undefined): string {
  return column && isTextColumn(column) && typeof value === 'string' && value.trim() !== '' ? value : ''
}

function formatKeyValue(value: unknown): string {
  if (value === null) return 'null'
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}
