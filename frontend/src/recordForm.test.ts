import { describe, expect, it } from 'vitest'
import {
  buildCreatePayload,
  buildUpdatePayload,
  canCreateRecord,
  canUpdateRecord,
  initialFormState,
  isFormDirty,
  validateRecordForm,
} from './recordForm'
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

const resource: ResourceMetadata = {
  name: 'items',
  kind: 'table',
  primary_key: ['tenant_id', 'id'],
  foreign_keys: [],
  columns: [
    column('tenant_id', 'bigint', { primary_key: true }),
    column('id', 'bigint', { primary_key: true, default: "nextval('items_id_seq')", identity: 'always', editable: false }),
    column('title', 'text'),
    column('description', 'text', { nullable: true }),
    column('price', 'numeric(20,4)', { nullable: true }),
    column('active', 'boolean'),
    column('published_on', 'date', { nullable: true }),
    column('created_at', 'timestamp with time zone', { default: 'now()' }),
    column('external_id', 'uuid', { nullable: true }),
    column('payload', 'jsonb', { default: "'{}'::jsonb" }),
    column('state', 'item_state', { enum_values: ['draft', 'published'] }),
    column('data', 'bytea', { nullable: true, editable: false }),
  ],
}

describe('record form payloads', () => {
  it('omits generated/default fields and distinguishes null from empty text', () => {
    const state = initialFormState('create', resource)
    state.tenant_id.value = '9223372036854775807'
    state.title.value = ''
    state.price.isNull = false
    state.price.value = '12345678901234567890.0001'
    state.active.value = 'false'
    state.published_on.isNull = false
    state.published_on.value = '2025-01-02'
    state.payload.useDefault = false
    state.payload.value = '{"edition":1}'
    state.state.value = 'draft'

    expect(buildCreatePayload(resource, state)).toEqual({
      tenant_id: '9223372036854775807',
      title: '',
      description: null,
      price: '12345678901234567890.0001',
      active: false,
      published_on: '2025-01-02',
      external_id: null,
      payload: { edition: 1 },
      state: 'draft',
    })
    expect(state.id.useDefault).toBe(true)
    expect(state.created_at.useDefault).toBe(true)
  })

  it('updates only changed editable non-primary-key fields', () => {
    const record = {
      tenant_id: '7', id: '9', title: 'Before', description: null, price: '10.20', active: true,
      published_on: null, created_at: '2025-01-01T00:00:00Z', external_id: null, payload: {}, state: 'draft', data: 'AAH/',
    }
    const initial = initialFormState('edit', resource, record)
    const state = structuredClone(initial)
    state.title.value = 'After'
    state.description.isNull = false
    state.description.value = ''
    state.tenant_id.value = '8'

    expect(buildUpdatePayload(resource, state, initial)).toEqual({ title: 'After', description: '' })
    expect(isFormDirty(state, initial)).toBe(true)
    expect(isFormDirty(initial, initial)).toBe(false)
  })
})

describe('record form validation', () => {
  it('validates numbers, dates, timestamps, UUID and JSON', () => {
    const state = initialFormState('create', resource)
    state.tenant_id.value = '1.5'
    state.title.value = 'Title'
    state.published_on.isNull = false
    state.published_on.value = '2025-02-30'
    state.created_at.useDefault = false
    state.created_at.value = 'not-a-timestamp'
    state.external_id.isNull = false
    state.external_id.value = 'invalid'
    state.payload.useDefault = false
    state.payload.value = '{invalid}'
    state.price.isNull = false
    state.price.value = '1,25'

    const errors = validateRecordForm('create', resource, state)
    expect(Object.keys(errors)).toEqual(expect.arrayContaining(['tenant_id', 'published_on', 'created_at', 'external_id', 'payload', 'price']))
  })

  it.each([
    { name: 'empty string', value: '' },
    { name: 'whitespace', value: ' \t\n ' },
  ])('rejects $name for required text', ({ value }) => {
    const state = initialFormState('create', resource)
    state.tenant_id.value = '1'
    state.title.value = value
    state.state.value = 'draft'
    expect(validateRecordForm('create', resource, state)).toEqual({ title: 'This field is required' })
  })

  it('rejects null for required text', () => {
    const state = initialFormState('create', resource)
    state.tenant_id.value = '1'
    state.title.isNull = true
    state.state.value = 'draft'
    expect(validateRecordForm('create', resource, state)).toEqual({ title: 'This field is required' })
  })

  it('rejects a missing required value', () => {
    const state = initialFormState('create', resource)
    state.tenant_id.value = '1'
    state.state.value = 'draft'
    delete state.title
    expect(validateRecordForm('create', resource, state)).toEqual({ title: 'This field is required' })
  })

  it('accepts valid required text without trimming it', () => {
    const state = initialFormState('create', resource)
    state.tenant_id.value = '1'
    state.title.value = '  Valid title  '
    state.state.value = 'draft'
    expect(validateRecordForm('create', resource, state)).toEqual({})
    expect(buildCreatePayload(resource, state).title).toBe('  Valid title  ')
  })

  it('keeps empty optional text distinct from null', () => {
    const state = initialFormState('create', resource)
    state.tenant_id.value = '1'
    state.title.value = 'Title'
    state.state.value = 'draft'
    state.description.isNull = false
    state.description.value = ''
    expect(validateRecordForm('create', resource, state)).toEqual({})
    expect(buildCreatePayload(resource, state).description).toBe('')
    state.description.isNull = true
    expect(buildCreatePayload(resource, state).description).toBeNull()
  })
})

describe('read-only state', () => {
  it('hides controls without permissions, write configuration or supported resource metadata', () => {
    expect(canCreateRecord(true, resource, false)).toBe(false)
    expect(canUpdateRecord(true, resource, false)).toBe(false)
    expect(canCreateRecord(false, resource, true)).toBe(false)
    expect(canUpdateRecord(false, resource, true)).toBe(false)
    expect(canCreateRecord(true, resource, true)).toBe(true)
    expect(canUpdateRecord(true, resource, true)).toBe(true)
    expect(canUpdateRecord(true, { ...resource, primary_key: [] }, true)).toBe(false)
    expect(canCreateRecord(true, { ...resource, kind: 'view' }, true)).toBe(false)
  })
})
