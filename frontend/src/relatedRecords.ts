import { can, handleUnauthorized } from './auth'
import {
  findReferencedResource,
  relationOption,
  relationValuesKey,
  type RelationOption,
} from './relation'
import { filterOperators, type RecordFilter, type RecordRow } from './records'
import { readableColumns } from './schema'
import type {
  ForeignKeyMetadata,
  ResourceMetadata,
  ReverseRelationMetadata,
  SchemaMetadata,
} from './schema'

export const MAX_RESOLVE_RELATIONS = 20
export const MAX_RESOLVE_KEYS = 200
export const MAX_RESOLVE_TOTAL_KEYS = 1000

export interface RelatedRelationPlan {
  foreignKey: ForeignKeyMetadata
  referencedResource: ResourceMetadata
  keys: RecordRow[]
}

interface ResolveRelationRequest {
  name: string
  keys: RecordRow[]
}

interface ResolveRequest {
  relations: ResolveRelationRequest[]
}

export class RelatedRecordsRequestError extends Error {
  constructor(readonly code: string, readonly status: number) {
    super(code)
    this.name = 'RelatedRecordsRequestError'
  }
}

export function localRelationKey(foreignKey: ForeignKeyMetadata, record: RecordRow): string {
  return relationValuesKey(foreignKey.columns.map((column) => record[column]))
}

export function buildRelatedRelationPlans(
  resource: ResourceMetadata,
  records: RecordRow[],
  schemas: SchemaMetadata[],
): RelatedRelationPlan[] {
  const plans: RelatedRelationPlan[] = []
  const localReadable = new Set(readableColumns(resource).map((column) => column.name))
  for (const foreignKey of resource.foreign_keys) {
    if (foreignKey.columns.length === 0 || foreignKey.columns.length !== foreignKey.referenced_columns.length) continue
    if (foreignKey.columns.some((column) => !localReadable.has(column))) continue
    const referencedResource = findReferencedResource(
      schemas,
      foreignKey.referenced_schema,
      foreignKey.referenced_table,
    )
    if (!referencedResource || !can(foreignKey.referenced_schema, foreignKey.referenced_table, 'read')) continue
    const referencedReadable = new Set(readableColumns(referencedResource).map((column) => column.name))
    if (foreignKey.referenced_columns.some((column) => !referencedReadable.has(column))) continue
    const unique = new Map<string, RecordRow>()
    for (const record of records) {
      const values = foreignKey.columns.map((column) => record[column])
      if (values.some((value) => value === null || value === undefined)) continue
      const key = relationValuesKey(values)
      if (!unique.has(key)) {
        unique.set(key, Object.fromEntries(
          foreignKey.referenced_columns.map((column, index) => [column, values[index]]),
        ))
      }
    }
    plans.push({ foreignKey, referencedResource, keys: [...unique.values()] })
  }
  return plans
}

export function chunkRelatedRelationPlans(plans: RelatedRelationPlan[]): RelatedRelationPlan[][] {
  const batches: RelatedRelationPlan[][] = []
  let current: RelatedRelationPlan[] = []
  let totalKeys = 0
  const flush = () => {
    if (current.length) batches.push(current)
    current = []
    totalKeys = 0
  }
  for (const plan of plans) {
    if (plan.keys.length === 0) {
      if (current.length < MAX_RESOLVE_RELATIONS) current.push({ ...plan, keys: [] })
      else {
        flush()
        current.push({ ...plan, keys: [] })
      }
      continue
    }
    for (let offset = 0; offset < plan.keys.length; offset += MAX_RESOLVE_KEYS) {
      const keys = plan.keys.slice(offset, offset + MAX_RESOLVE_KEYS)
      if (
        current.length >= MAX_RESOLVE_RELATIONS ||
        totalKeys + keys.length > MAX_RESOLVE_TOTAL_KEYS ||
        current.some((item) => item.foreignKey.name === plan.foreignKey.name)
      ) flush()
      current.push({ ...plan, keys })
      totalKeys += keys.length
      if (offset + MAX_RESOLVE_KEYS < plan.keys.length) flush()
    }
  }
  flush()
  return batches
}

export async function fetchRelatedLabels(
  backendBaseURL: string,
  schemaName: string,
  resourceName: string,
  plans: RelatedRelationPlan[],
  signal?: AbortSignal,
): Promise<Map<string, Map<string, RelationOption>>> {
  const labels = new Map<string, Map<string, RelationOption>>()
  for (const batch of chunkRelatedRelationPlans(plans)) {
    const body: ResolveRequest = {
      relations: batch.map((plan) => ({ name: plan.foreignKey.name, keys: plan.keys })),
    }
    const response = await fetch(
      `${backendBaseURL}/api/resources/${encodeURIComponent(schemaName)}/${encodeURIComponent(resourceName)}/relations/resolve`,
      {
        method: 'POST',
        headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
        credentials: 'same-origin',
        body: JSON.stringify(body),
        signal,
      },
    )
    const responseBody: unknown = await response.json()
    if (response.status === 401) handleUnauthorized()
    if (!response.ok) {
      const code = isRecord(responseBody) && isRecord(responseBody.error) && typeof responseBody.error.code === 'string'
        ? responseBody.error.code
        : 'RELATION_RESOLVE_FAILED'
      throw new RelatedRecordsRequestError(code, response.status)
    }
    if (!isRecord(responseBody) || !Array.isArray(responseBody.relations)) throw new Error('Invalid relation resolve response')
    for (const relation of responseBody.relations) {
      if (!isRecord(relation) || typeof relation.name !== 'string' || !Array.isArray(relation.records)) {
        throw new Error('Invalid relation resolve response')
      }
      const plan = batch.find((item) => item.foreignKey.name === relation.name)
      if (!plan) throw new Error('Invalid relation resolve response')
      const relationLabels = labels.get(plan.foreignKey.name) ?? new Map<string, RelationOption>()
      for (const value of relation.records) {
        if (!isRecord(value)) throw new Error('Invalid relation resolve response')
        const option = relationOption(value, plan.referencedResource, plan.foreignKey.referenced_columns)
        relationLabels.set(option.key, option)
      }
      labels.set(plan.foreignKey.name, relationLabels)
    }
  }
  return labels
}

export interface ReverseRelationView {
  relation: ReverseRelationMetadata
  childResource: ResourceMetadata
}

export function readableReverseRelations(
  parentResource: ResourceMetadata,
  schemas: SchemaMetadata[],
): ReverseRelationView[] {
  const relations: ReverseRelationView[] = []
  for (const relation of parentResource.reverse_relations ?? []) {
    if (relation.columns.length === 0 || relation.columns.length !== relation.referenced_columns.length) continue
    const childResource = findReferencedResource(schemas, relation.schema, relation.resource)
    if (!childResource || !can(relation.schema, relation.resource, 'read')) continue
    const childColumns = relation.columns.map((name) => readableColumns(childResource).find((column) => column.name === name))
    const parentColumns = relation.referenced_columns.map((name) => readableColumns(parentResource).find((column) => column.name === name))
    if (childColumns.some((column) => !column) || parentColumns.some((column) => !column)) continue
    if (!parentColumns.every((column) => column && filterOperators(column.data_type).includes('eq'))) continue
    relations.push({ relation, childResource })
  }
  return relations
}

export function reverseRelationParentFilters(
  relation: ReverseRelationMetadata,
  parentRecord: RecordRow | undefined,
): RecordFilter[] | null {
  if (!parentRecord) return null
  const filters: RecordFilter[] = []
  for (let index = 0; index < relation.columns.length; index++) {
    const value = parentRecord[relation.referenced_columns[index]]
    if (value === null || value === undefined || typeof value === 'object') return null
    filters.push({ column: relation.columns[index], operator: 'eq', value: String(value) })
  }
  return filters.length > 0 ? filters : null
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
