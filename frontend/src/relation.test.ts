import { describe, expect, it } from 'vitest'
import { exactRelationFilters, findReferencedResource, relationOption, relationValuesKey } from './relation'
import { initialFormState, validateRecordForm } from './recordForm'
import type { ColumnMetadata, ResourceMetadata } from './schema'

function column(name: string, dataType: string, overrides: Partial<ColumnMetadata> = {}): ColumnMetadata {
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

const referenced: ResourceMetadata = {
  name: 'referenced',
  kind: 'table',
  primary_key: ['tenant_id', 'id'],
  foreign_keys: [],
  columns: [
    column('tenant_id', 'bigint'),
    column('id', 'uuid'),
    column('display_name', 'text'),
    column('name', 'text'),
    column('description', 'text'),
  ],
}

describe('relation metadata and labels', () => {
  it('resolves resources only from the supplied authorized schema catalog', () => {
    const schemas = [{ name: 'public', resources: [referenced] }]
    expect(findReferencedResource(schemas, 'public', 'referenced')).toBe(referenced)
    expect(findReferencedResource(schemas, 'private', 'secrets')).toBeNull()
  })

  it('uses display label priority and shows every composite key part', () => {
    const option = relationOption({
      tenant_id: '9223372036854775807',
      id: '00112233-4455-6677-8899-aabbccddeeff',
      display_name: 'Display',
      name: 'Name',
      title: 'Title',
    }, referenced, ['tenant_id', 'id'])

    expect(option.label).toBe('Display')
    expect(option.keyLabel).toBe('tenant_id=9223372036854775807 · id=00112233-4455-6677-8899-aabbccddeeff')
    expect(option.key).toBe('["9223372036854775807","00112233-4455-6677-8899-aabbccddeeff"]')
  })

  it('falls back to another text field and then the technical identifier', () => {
    expect(relationOption({ id: '1', description: 'Description' }, referenced, ['id']).label).toBe('Description')
    expect(relationOption({ id: '1', description: '' }, referenced, ['id']).label).toBe('id=1')
  })

  it('matches integer API numbers with form strings without changing stored values', () => {
    expect(relationValuesKey([1])).toBe(relationValuesKey(['1']))
    expect(relationValuesKey(['9223372036854775807'])).toBe('["9223372036854775807"]')
    expect(relationValuesKey([null])).toBe('[null]')
  })

  it('builds exact typed filters without bigint, UUID or null coercion', () => {
    expect(exactRelationFilters(['tenant_id', 'id'], [
      '9223372036854775807',
      '00112233-4455-6677-8899-aabbccddeeff',
    ])).toEqual([
      { column: 'tenant_id', operator: 'eq', value: '9223372036854775807' },
      { column: 'id', operator: 'eq', value: '00112233-4455-6677-8899-aabbccddeeff' },
    ])
  })
})

describe('composite relation validation', () => {
  const local: ResourceMetadata = {
    name: 'local',
    kind: 'table',
    primary_key: ['id'],
    foreign_keys: [{
      name: 'local_parent_fk',
      columns: ['tenant_id', 'parent_id'],
      referenced_schema: 'public',
      referenced_table: 'referenced',
      referenced_columns: ['tenant_id', 'id'],
    }],
    columns: [
      column('id', 'bigint', { primary_key: true, editable: false }),
      column('tenant_id', 'bigint', { nullable: true }),
      column('parent_id', 'uuid', { nullable: true }),
    ],
  }

  it('rejects a partially populated composite relation', () => {
    const state = initialFormState('create', local)
    state.tenant_id.isNull = false
    state.tenant_id.value = '1'
    state.parent_id.isNull = true

    expect(validateRecordForm('create', local, state)).toEqual({
      tenant_id: 'validation.incompleteRelation',
      parent_id: 'validation.incompleteRelation',
    })
  })

  it('allows a fully cleared nullable composite relation', () => {
    const state = initialFormState('create', local)
    expect(validateRecordForm('create', local, state)).toEqual({})
  })

  it('requires the relation when any part is not nullable', () => {
    const required = {
      ...local,
      columns: local.columns.map((item) => item.name === 'tenant_id' ? { ...item, nullable: false } : item),
    }
    const state = initialFormState('create', required)
    state.parent_id.isNull = false

    expect(validateRecordForm('create', required, state)).toEqual({
      tenant_id: 'validation.requiredRelation',
      parent_id: 'validation.requiredRelation',
    })
  })
})
