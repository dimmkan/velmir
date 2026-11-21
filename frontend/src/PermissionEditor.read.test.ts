// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import PermissionEditor from './PermissionEditor.vue'
import { createVelmirI18n } from './i18n'
import type { ResourcePermission } from './auth'
import type { ResourceMetadata } from './schema'

const resource: ResourceMetadata = {
  name: 'books', kind: 'table', primary_key: ['id'], foreign_keys: [],
  columns: [
    { name: 'id', data_type: 'bigint', nullable: false, default: null, primary_key: true, identity: 'always', generated: false, editable: false, enum_values: [] },
    { name: 'title', data_type: 'text', nullable: false, default: null, primary_key: false, identity: '', generated: false, editable: true, enum_values: [] },
    { name: 'generated', data_type: 'text', nullable: true, default: null, primary_key: false, identity: '', generated: true, editable: false, enum_values: [] },
  ],
}

describe('PermissionEditor read fields', () => {
  it('locks PK, permits generated/read-only columns and describes an empty allowlist as PK-only', async () => {
    const permissions: ResourcePermission[] = [{
      schema: 'public', resource: 'books', operations: ['read'],
      field_permissions: [{ action: 'read', mode: 'allowlist', columns: [] }],
    }]
    const wrapper = mount(PermissionEditor, {
      props: { permissions, schemas: [{ name: 'public', resources: [resource] }] },
      global: { plugins: [createVelmirI18n()] },
    })
    const inputs = wrapper.findAll('.field-permission-columns input')
    expect(inputs).toHaveLength(3)
    expect((inputs[0].element as HTMLInputElement).checked).toBe(true)
    expect((inputs[0].element as HTMLInputElement).disabled).toBe(true)
    expect(wrapper.text()).toContain('Только primary key')
    await inputs[2].setValue(true)
    const emitted = wrapper.emitted('change')!.at(-1)![0] as ResourcePermission[]
    expect(emitted[0].field_permissions?.[0].columns).toEqual(['generated'])
  })
})
