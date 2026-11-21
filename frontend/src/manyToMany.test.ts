import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { authState, handleUnauthorized } from './auth'
import {
  canAddManyToManyRelation,
  canRemoveManyToManyRelation,
  detectManyToManyRelation,
  manyToManyCreateValues,
  manyToManyParentFilters,
  readableManyToManyRelations,
} from './manyToMany'
import type { ColumnMetadata, ForeignKeyMetadata, ResourceMetadata } from './schema'

function column(name: string, dataType = 'text', overrides: Partial<ColumnMetadata> = {}): ColumnMetadata {
  return {
    name,
    data_type: dataType,
    nullable: false,
    default: null,
    primary_key: false,
    identity: '',
    generated: false,
    editable: true,
    enum_values: [],
    ...overrides,
  }
}

function foreignKey(
  name: string,
  columns: string[],
  target: string,
  referencedColumns: string[],
): ForeignKeyMetadata {
  return {
    name,
    columns,
    referenced_schema: 'public',
    referenced_table: target,
    referenced_columns: referencedColumns,
  }
}

const parents: ResourceMetadata = {
  name: 'parents',
  kind: 'table',
  primary_key: ['id'],
  foreign_keys: [],
  columns: [column('id', 'bigint'), column('name')],
}
const targets: ResourceMetadata = {
  name: 'targets',
  kind: 'table',
  primary_key: ['id'],
  foreign_keys: [],
  columns: [column('id', 'bigint'), column('name')],
}
const junction: ResourceMetadata = {
  name: 'parent_targets',
  kind: 'table',
  primary_key: ['parent_id', 'target_id'],
  foreign_keys: [
    foreignKey('parent_targets_parent_fk', ['parent_id'], 'parents', ['id']),
    foreignKey('parent_targets_target_fk', ['target_id'], 'targets', ['id']),
  ],
  columns: [
    column('parent_id', 'bigint'),
    column('target_id', 'bigint'),
    column('note', 'text', { nullable: true }),
  ],
}
const schemas = [{ name: 'public', resources: [parents, targets, junction] }]

beforeEach(() => {
  authState.status = 'authenticated'
  authState.permissions = [{ schema: '*', resource: '*', operations: ['read', 'create', 'delete'] }]
})

afterEach(() => handleUnauthorized())

describe('generic many-to-many detection', () => {
  it('accepts a junction whose composite PK is exactly both FKs and allows optional/default/generated attributes', () => {
    const supported: ResourceMetadata = {
      ...junction,
      columns: [
        ...junction.columns,
        column('created_at', 'timestamp with time zone', { default: 'now()' }),
        column('computed', 'text', { generated: true, editable: false }),
      ],
    }
    const relation = detectManyToManyRelation('public', parents, 'public', supported, [
      { name: 'public', resources: [parents, targets, supported] },
    ])
    expect(relation).toMatchObject({
      junctionSchema: 'public',
      junctionResource: supported,
      targetResource: targets,
      parentForeignKey: supported.foreign_keys[0],
      targetForeignKey: supported.foreign_keys[1],
    })
    expect(readableManyToManyRelations('public', parents, schemas)).toHaveLength(1)
  })

  it.each([
    ['an additional required attribute', { ...junction, columns: [...junction.columns, column('required_note')] }],
    ['a surrogate PK', { ...junction, primary_key: ['id'], columns: [column('id', 'bigint'), ...junction.columns] }],
    ['three FKs', { ...junction, foreign_keys: [...junction.foreign_keys, foreignKey('other_fk', ['other_id'], 'targets', ['id'])], columns: [...junction.columns, column('other_id', 'bigint')] }],
    ['a self-referential target pair', { ...junction, foreign_keys: [junction.foreign_keys[0], foreignKey('other_parent_fk', ['target_id'], 'parents', ['id'])] }],
    ['a view', { ...junction, kind: 'view' as const }],
    ['a non-editable FK column', { ...junction, columns: [column('parent_id', 'bigint', { editable: false }), junction.columns[1]] }],
    ['a missing target resource', junction],
  ])('rejects %s', (_name, candidate) => {
    const available = candidate === junction ? [parents, junction] : [parents, targets, candidate]
    expect(detectManyToManyRelation('public', parents, 'public', candidate, [{ name: 'public', resources: available }])).toBeNull()
  })

  it('supports composite parent and target keys in any PK order without numeric coercion', () => {
    const compositeParents: ResourceMetadata = {
      ...parents,
      primary_key: ['tenant_id', 'external_id'],
      columns: [column('tenant_id', 'bigint'), column('external_id', 'uuid'), column('name')],
    }
    const compositeTargets: ResourceMetadata = {
      ...targets,
      primary_key: ['tenant_id', 'code'],
      columns: [column('tenant_id', 'bigint'), column('code', 'uuid'), column('name')],
    }
    const compositeJunction: ResourceMetadata = {
      name: 'composite_links',
      kind: 'table',
      primary_key: ['target_tenant_id', 'target_code', 'parent_tenant_id', 'parent_external_id'],
      foreign_keys: [
        foreignKey('composite_parent_fk', ['parent_tenant_id', 'parent_external_id'], 'parents', ['tenant_id', 'external_id']),
        foreignKey('composite_target_fk', ['target_tenant_id', 'target_code'], 'targets', ['tenant_id', 'code']),
      ],
      columns: [
        column('parent_tenant_id', 'bigint'), column('parent_external_id', 'uuid'),
        column('target_tenant_id', 'bigint'), column('target_code', 'uuid'),
      ],
    }
    const relation = detectManyToManyRelation('public', compositeParents, 'public', compositeJunction, [
      { name: 'public', resources: [compositeParents, compositeTargets, compositeJunction] },
    ])!
    const parent = { tenant_id: '9223372036854775807', external_id: '00112233-4455-6677-8899-aabbccddeeff' }
    const target = { tenant_id: '9223372036854775806', code: '11112233-4455-6677-8899-aabbccddeeff' }
    expect(manyToManyParentFilters(relation, parent)).toEqual([
      { column: 'parent_tenant_id', operator: 'eq', value: '9223372036854775807' },
      { column: 'parent_external_id', operator: 'eq', value: '00112233-4455-6677-8899-aabbccddeeff' },
    ])
    expect(manyToManyCreateValues(relation, parent, target)).toEqual({
      parent_tenant_id: '9223372036854775807',
      parent_external_id: '00112233-4455-6677-8899-aabbccddeeff',
      target_tenant_id: '9223372036854775806',
      target_code: '11112233-4455-6677-8899-aabbccddeeff',
    })
  })

  it('uses RBAC-filtered metadata and hides junction/target resources without read', () => {
    authState.permissions = [{ schema: 'public', resource: 'parents', operations: ['read'] }]
    expect(readableManyToManyRelations('public', parents, schemas)).toEqual([])
    expect(readableManyToManyRelations('public', parents, [{ name: 'public', resources: [parents, targets] }])).toEqual([])
    expect(readableManyToManyRelations('public', parents, [{ name: 'public', resources: [parents, junction] }])).toEqual([])
  })

  it('requires writer and independent junction create/delete grants without parent/target update', () => {
    const relation = detectManyToManyRelation('public', parents, 'public', junction, schemas)!
    authState.permissions = [
      { schema: 'public', resource: 'parents', operations: ['read'] },
      { schema: 'public', resource: 'targets', operations: ['read'] },
      { schema: 'public', resource: 'parent_targets', operations: ['read', 'create'] },
    ]
    expect(canAddManyToManyRelation(relation, true)).toBe(true)
    expect(canAddManyToManyRelation(relation, false)).toBe(false)
    expect(canRemoveManyToManyRelation(relation, true)).toBe(false)
    authState.permissions[2].operations = ['read', 'delete']
    expect(canAddManyToManyRelation(relation, true)).toBe(false)
    expect(canRemoveManyToManyRelation(relation, true)).toBe(true)
    expect(canRemoveManyToManyRelation(relation, false)).toBe(false)
  })

  it('requires every junction FK column in effective creatable fields', () => {
    const relation = detectManyToManyRelation('public', parents, 'public', junction, schemas)!
    authState.permissions = [
      { schema: 'public', resource: 'parents', operations: ['read'] },
      { schema: 'public', resource: 'targets', operations: ['read'] },
      { schema: 'public', resource: 'parent_targets', operations: ['read', 'create', 'delete'] },
    ]
    const restricted = {
      ...relation,
      junctionResource: {
        ...relation.junctionResource,
        creatable_fields: [...relation.parentForeignKey.columns],
        can_create: false,
      },
    }
    expect(canAddManyToManyRelation(restricted, true)).toBe(false)
    expect(canRemoveManyToManyRelation(restricted, true)).toBe(true)
    restricted.junctionResource.creatable_fields = [
      ...relation.parentForeignKey.columns,
      ...relation.targetForeignKey.columns,
    ]
    restricted.junctionResource.can_create = true
    expect(canAddManyToManyRelation(restricted, true)).toBe(true)
  })
})
