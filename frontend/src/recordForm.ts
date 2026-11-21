import type { RecordRow } from './records'
import type { ColumnMetadata, ResourceMetadata } from './schema'
import type { RecordValues } from './mutations'

export type FormMode = 'create' | 'edit'

export interface FormFieldState {
  value: string
  isNull: boolean
  useDefault: boolean
}

export type RecordFormState = Record<string, FormFieldState>
export type FormErrors = Record<string, string>

export function canCreateRecord(writeEnabled: boolean, resource: ResourceMetadata, permissionAllowed: boolean): boolean {
  return permissionAllowed && writeEnabled && resource.kind === 'table' && resource.can_create !== false
}

export function canUpdateRecord(writeEnabled: boolean, resource: ResourceMetadata, permissionAllowed: boolean): boolean {
  const structurallyUpdatable = resource.primary_key.length > 0 && resource.columns.some((column) => column.editable && !column.primary_key)
  return permissionAllowed && writeEnabled && resource.kind === 'table' && structurallyUpdatable && resource.can_update !== false
}

export function writableFieldNames(mode: FormMode, resource: ResourceMetadata): Set<string> {
  const fields = mode === 'create' ? resource.creatable_fields : resource.updatable_fields
  if (fields) return new Set(fields)
  return new Set(resource.columns.filter((column) =>
    column.editable && !(mode === 'edit' && column.primary_key),
  ).map((column) => column.name))
}

export function initialFormState(mode: FormMode, resource: ResourceMetadata, record?: RecordRow): RecordFormState {
  const state: RecordFormState = {}
  for (const column of resource.columns) {
    const value = mode === 'edit' ? record?.[column.name] : undefined
    const generated = column.generated || column.identity === 'always'
    state[column.name] = {
      value: inputValue(value, column),
      isNull: mode === 'edit' ? value === null : column.nullable && column.default === null && !generated,
      useDefault: mode === 'create' && (column.default !== null || column.identity !== '' || generated),
    }
  }
  return state
}

export function buildCreatePayload(resource: ResourceMetadata, state: RecordFormState): RecordValues {
  const values: RecordValues = {}
  const allowed = writableFieldNames('create', resource)
  for (const column of resource.columns) {
    const field = state[column.name]
    if (!allowed.has(column.name) || !column.editable || !field || field.useDefault) continue
    values[column.name] = fieldValue(column, field)
  }
  return values
}

export function buildUpdatePayload(
  resource: ResourceMetadata,
  state: RecordFormState,
  initial: RecordFormState,
): RecordValues {
  const values: RecordValues = {}
  const allowed = writableFieldNames('edit', resource)
  for (const column of resource.columns) {
    const field = state[column.name]
    if (!allowed.has(column.name) || !column.editable || column.primary_key || !field || !initial[column.name] || fieldsEqual(field, initial[column.name])) continue
    values[column.name] = fieldValue(column, field)
  }
  return values
}

export function validateRecordForm(
  mode: FormMode,
  resource: ResourceMetadata,
  state: RecordFormState,
): FormErrors {
  const errors: FormErrors = {}
  const allowed = writableFieldNames(mode, resource)
  for (const column of resource.columns) {
    if (!allowed.has(column.name) || !column.editable || (mode === 'edit' && column.primary_key)) continue
    const field = state[column.name]
    if (!field) {
      if (!column.nullable) errors[column.name] = 'validation.required'
      continue
    }
    if (field.useDefault) continue
    if (field.isNull) {
      if (!column.nullable) errors[column.name] = isTextColumn(column) ? 'validation.required' : 'validation.notNull'
      continue
    }
    const value = field.value
    if (!column.nullable && (value === '' || isTextColumn(column) && value.trim() === '')) {
      errors[column.name] = 'validation.required'
      continue
    }
    if (value === '') continue
    const error = validateType(column, value)
    if (error) errors[column.name] = error
  }
  for (const foreignKey of resource.foreign_keys) {
    if (foreignKey.columns.length === 0 || foreignKey.columns.length !== foreignKey.referenced_columns.length) continue
    const columns = foreignKey.columns.map((name) => resource.columns.find((column) => column.name === name))
    if (columns.some((column) => !column) || foreignKey.columns.some((name) => !allowed.has(name))) continue
    const fields = foreignKey.columns.map((name) => state[name])
    if (fields.some((field) => !field)) continue

    const defaultCount = fields.filter((field) => field.useDefault).length
    if (defaultCount === fields.length) continue
    const nullCount = fields.filter((field) => field.isNull).length
    const present = fields.map((field) => !field.useDefault && !field.isNull && field.value !== '')
    const presentCount = present.filter(Boolean).length
    const editableColumns = columns.filter((column): column is ColumnMetadata =>
      Boolean(column?.editable && !(mode === 'edit' && column.primary_key)),
    )

    if (foreignKey.columns.length > 1 && (
      presentCount > 0 && presentCount < fields.length ||
      nullCount > 0 && nullCount < fields.length ||
      defaultCount > 0
    )) {
      for (const column of editableColumns) errors[column.name] = 'validation.incompleteRelation'
    } else if (presentCount === 0 && columns.some((column) => !column?.nullable)) {
      for (const column of editableColumns) errors[column.name] = 'validation.requiredRelation'
    }
  }
  return errors
}

export function isFormDirty(state: RecordFormState, initial: RecordFormState): boolean {
  return Object.keys(state).some((name) => !fieldsEqual(state[name], initial[name]))
}

export function cloneFormState(state: RecordFormState): RecordFormState {
  const clone: RecordFormState = {}
  for (const [name, field] of Object.entries(state)) {
    clone[name] = {
      value: field.value,
      isNull: field.isNull,
      useDefault: field.useDefault,
    }
  }
  return clone
}

function fieldValue(column: ColumnMetadata, field: FormFieldState): unknown {
  if (field.isNull) return null
  const type = baseType(column.data_type)
  if (type === 'boolean') return field.value === 'true'
  if (['smallint', 'integer', 'real', 'double precision'].includes(type)) return Number(field.value)
  if (['json', 'jsonb'].includes(type)) return JSON.parse(field.value || 'null')
  return field.value
}

function inputValue(value: unknown, column: ColumnMetadata): string {
  if (value === null || value === undefined) return typeDefault(column)
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

function typeDefault(column: ColumnMetadata): string {
  if (baseType(column.data_type) === 'boolean') return 'true'
  if (['json', 'jsonb'].includes(baseType(column.data_type)) && !column.nullable && column.default === null) return '{}'
  return ''
}

function validateType(column: ColumnMetadata, value: string): string {
  const type = baseType(column.data_type)
  if (column.enum_values.length > 0 && !column.enum_values.includes(value)) return 'validation.allowedValue'
  if (['smallint', 'integer', 'bigint'].includes(type)) {
    if (!/^[+-]?\d+$/.test(value)) return 'validation.integer'
    const parsed = BigInt(value)
    const limits: Record<string, [bigint, bigint]> = {
      smallint: [-32768n, 32767n],
      integer: [-2147483648n, 2147483647n],
      bigint: [-9223372036854775808n, 9223372036854775807n],
    }
    const [minimum, maximum] = limits[type]
    if (parsed < minimum || parsed > maximum) return 'validation.integerRange'
  }
  if (['real', 'double precision'].includes(type) && (value.trim() === '' || !Number.isFinite(Number(value)))) return 'validation.finiteNumber'
  if (['numeric', 'decimal'].includes(type) && !/^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?$/.test(value)) return 'validation.decimal'
  if (type === 'date' && !validDate(value)) return 'validation.date'
  if (type === 'timestamp without time zone' && !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?$/.test(value)) return 'validation.timestamp'
  if (type === 'timestamp with time zone' && Number.isNaN(Date.parse(value))) return 'validation.timestampTz'
  if (type === 'uuid' && !/^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/.test(value)) return 'validation.uuid'
  if (['json', 'jsonb'].includes(type)) {
    try {
      JSON.parse(value)
    } catch {
      return 'validation.json'
    }
  }
  return ''
}

function validDate(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false
  const date = new Date(`${value}T00:00:00Z`)
  return !Number.isNaN(date.getTime()) && date.toISOString().slice(0, 10) === value
}

function fieldsEqual(left: FormFieldState, right: FormFieldState): boolean {
  return left.value === right.value && left.isNull === right.isNull && left.useDefault === right.useDefault
}

function baseType(dataType: string): string {
  return dataType.toLowerCase().trim().replace(/\([^)]*\)/g, '').trim()
}

function isTextColumn(column: ColumnMetadata): boolean {
  return ['text', 'character varying', 'character', 'citext', 'name'].includes(baseType(column.data_type))
}
