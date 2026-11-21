import { can } from './auth'
import { findReferencedResource, relationValuesKey } from './relation'
import { filterOperators, type RecordFilter, type RecordRow } from './records'
import { readableColumns, type ForeignKeyMetadata, type ResourceMetadata, type SchemaMetadata } from './schema'

export interface ManyToManyRelation {
  id: string
  junctionSchema: string
  junctionResource: ResourceMetadata
  parentForeignKey: ForeignKeyMetadata
  targetForeignKey: ForeignKeyMetadata
  targetResource: ResourceMetadata
}

export function readableManyToManyRelations(
  parentSchema: string,
  parentResource: ResourceMetadata,
  schemas: SchemaMetadata[],
): ManyToManyRelation[] {
  if (!can(parentSchema, parentResource.name, 'read')) return []
  const relations: ManyToManyRelation[] = []
  for (const schema of schemas) {
    for (const junctionResource of schema.resources) {
      const relation = detectManyToManyRelation(parentSchema, parentResource, schema.name, junctionResource, schemas)
      if (relation) relations.push(relation)
    }
  }
  return relations
}

export function detectManyToManyRelation(
  parentSchema: string,
  parentResource: ResourceMetadata,
  junctionSchema: string,
  junctionResource: ResourceMetadata,
  schemas: SchemaMetadata[],
): ManyToManyRelation | null {
  if (junctionResource.kind !== 'table' || junctionResource.foreign_keys.length !== 2) return null
  if (!can(junctionSchema, junctionResource.name, 'read')) return null
  if (junctionSchema === parentSchema && junctionResource.name === parentResource.name) return null

  const parentIndex = junctionResource.foreign_keys.findIndex((foreignKey) =>
    foreignKey.referenced_schema === parentSchema && foreignKey.referenced_table === parentResource.name,
  )
  if (parentIndex < 0) return null
  const parentForeignKey = junctionResource.foreign_keys[parentIndex]
  const targetForeignKey = junctionResource.foreign_keys[parentIndex === 0 ? 1 : 0]
  if (
    targetForeignKey.referenced_schema === parentSchema &&
    targetForeignKey.referenced_table === parentResource.name
  ) return null
  if (
    targetForeignKey.referenced_schema === junctionSchema &&
    targetForeignKey.referenced_table === junctionResource.name
  ) return null

  const targetResource = findReferencedResource(
    schemas,
    targetForeignKey.referenced_schema,
    targetForeignKey.referenced_table,
  )
  if (!targetResource || !can(targetForeignKey.referenced_schema, targetForeignKey.referenced_table, 'read')) return null
  if (!validForeignKey(parentForeignKey) || !validForeignKey(targetForeignKey)) return null

  const foreignKeyColumns = [...parentForeignKey.columns, ...targetForeignKey.columns]
  if (
    new Set(foreignKeyColumns).size !== foreignKeyColumns.length ||
    junctionResource.primary_key.length !== foreignKeyColumns.length ||
    new Set(junctionResource.primary_key).size !== junctionResource.primary_key.length ||
    foreignKeyColumns.some((column) => !junctionResource.primary_key.includes(column)) ||
    junctionResource.primary_key.some((column) => !foreignKeyColumns.includes(column))
  ) return null

  const junctionReadable = new Set(readableColumns(junctionResource).map((column) => column.name))
  if (foreignKeyColumns.some((name) => !junctionReadable.has(name))) return null
  const foreignKeyColumnSet = new Set(foreignKeyColumns)
  if (foreignKeyColumns.some((name) => {
    const column = junctionResource.columns.find((item) => item.name === name)
    return !column?.editable || column.generated || column.identity === 'always'
  })) return null
  if (junctionResource.columns.some((column) =>
    !foreignKeyColumnSet.has(column.name) &&
    !column.nullable &&
    column.default === null &&
    !column.generated &&
    column.identity === ''
  )) return null

  if (!supportedReferencedColumns(parentResource, parentForeignKey.referenced_columns)) return null
  if (!supportedReferencedColumns(targetResource, targetForeignKey.referenced_columns)) return null

  return {
    id: `${junctionSchema}.${junctionResource.name}.${parentForeignKey.name}.${targetForeignKey.name}`,
    junctionSchema,
    junctionResource,
    parentForeignKey,
    targetForeignKey,
    targetResource,
  }
}

export function manyToManyParentFilters(
  relation: ManyToManyRelation,
  parentRecord: RecordRow | undefined,
): RecordFilter[] | null {
  if (!parentRecord) return null
  const filters: RecordFilter[] = []
  for (let index = 0; index < relation.parentForeignKey.columns.length; index++) {
    const value = parentRecord[relation.parentForeignKey.referenced_columns[index]]
    if (value === null || value === undefined || typeof value === 'object') return null
    filters.push({
      column: relation.parentForeignKey.columns[index],
      operator: 'eq',
      value: String(value),
    })
  }
  return filters.length > 0 ? filters : null
}

export function manyToManyTargetKey(relation: ManyToManyRelation, junctionRecord: RecordRow): string {
  return relationValuesKey(relation.targetForeignKey.columns.map((column) => junctionRecord[column]))
}

export function manyToManyTargetKeyRecord(relation: ManyToManyRelation, junctionRecord: RecordRow): RecordRow {
  return Object.fromEntries(relation.targetForeignKey.referenced_columns.map((column, index) => [
    column,
    junctionRecord[relation.targetForeignKey.columns[index]],
  ]))
}

export function manyToManyCreateValues(
  relation: ManyToManyRelation,
  parentRecord: RecordRow,
  targetRecord: RecordRow,
): RecordRow | null {
  const values: RecordRow = {}
  for (const [foreignKey, source] of [
    [relation.parentForeignKey, parentRecord],
    [relation.targetForeignKey, targetRecord],
  ] as const) {
    for (let index = 0; index < foreignKey.columns.length; index++) {
      const value = source[foreignKey.referenced_columns[index]]
      if (value === null || value === undefined || typeof value === 'object') return null
      values[foreignKey.columns[index]] = value
    }
  }
  return values
}

export function canAddManyToManyRelation(relation: ManyToManyRelation, writeEnabled: boolean): boolean {
  const creatableFields = new Set(relation.junctionResource.creatable_fields ?? relation.junctionResource.columns.filter((column) => column.editable).map((column) => column.name))
  const requiredColumns = [...relation.parentForeignKey.columns, ...relation.targetForeignKey.columns]
  return writeEnabled && relation.junctionResource.can_create !== false &&
    requiredColumns.every((column) => creatableFields.has(column)) &&
    can(relation.junctionSchema, relation.junctionResource.name, 'create') &&
    can(relation.targetForeignKey.referenced_schema, relation.targetForeignKey.referenced_table, 'read')
}

export function canRemoveManyToManyRelation(relation: ManyToManyRelation, writeEnabled: boolean): boolean {
  return writeEnabled && can(relation.junctionSchema, relation.junctionResource.name, 'delete')
}

function validForeignKey(foreignKey: ForeignKeyMetadata): boolean {
  return foreignKey.columns.length > 0 && foreignKey.columns.length === foreignKey.referenced_columns.length
}

function supportedReferencedColumns(resource: ResourceMetadata, names: string[]): boolean {
  return names.length > 0 && names.every((name) => {
    const column = readableColumns(resource).find((item) => item.name === name)
    return column && filterOperators(column.data_type).includes('eq')
  })
}
