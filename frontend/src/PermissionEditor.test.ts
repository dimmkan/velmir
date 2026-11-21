// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import PermissionEditor from './PermissionEditor.vue'
import { createVelmirI18n } from './i18n'
import type { ResourcePermission } from './auth'
import type { ColumnMetadata, ResourceMetadata } from './schema'

function column(name: string, overrides: Partial<ColumnMetadata> = {}): ColumnMetadata {
  return {
    name, data_type: 'text', nullable: false, default: null, primary_key: false,
    identity: '', generated: false, editable: true, enum_values: [], ...overrides,
  }
}

const books: ResourceMetadata = {
  name: 'books', kind: 'table', primary_key: ['id'], foreign_keys: [],
  columns: [
    column('id', { primary_key: true }),
    column('title'),
    column('isbn'),
    column('identity_value', { identity: 'by_default' }),
    column('generated_value', { generated: true, editable: false }),
    column('read_only', { editable: false }),
  ],
}

function permissions(schema = 'public', resource = 'books'): ResourcePermission[] {
  return [{
    schema, resource, operations: ['read', 'create', 'update'],
    field_permissions: [
      { action: 'create', mode: 'unrestricted', columns: [] },
      { action: 'update', mode: 'unrestricted', columns: [] },
    ],
  }]
}

describe('PermissionEditor field permissions', () => {
  it('supports unrestricted, explicit empty allowlist and writable metadata columns', async () => {
    const i18n = createVelmirI18n()
    const wrapper = mount(PermissionEditor, {
      props: { permissions: permissions(), schemas: [{ name: 'public', resources: [books] }] },
      global: { plugins: [i18n] },
    })
    const radios = wrapper.findAll('input[type="radio"]')
    expect(radios).toHaveLength(4)
    expect((radios[0].element as HTMLInputElement).checked).toBe(true)
    await radios[3].setValue(true)
    let current = wrapper.emitted('change')!.at(-1)![0] as ResourcePermission[]
    expect(current[0].field_permissions![1]).toEqual({ action: 'update', mode: 'allowlist', columns: [] })
    await wrapper.setProps({ permissions: current })
    expect(wrapper.get('.empty-allowlist-warning').text()).toContain('Нет доступных полей')
    const labels = wrapper.findAll('.field-permission-columns label').map((label) => label.text())
    expect(labels).toEqual(['title', 'isbn'])

    await wrapper.get('.field-permission-columns input').setValue(true)
    current = wrapper.emitted('change')!.at(-1)![0] as ResourcePermission[]
    expect(current[0].field_permissions![1].columns).toEqual(['title'])
    await wrapper.setProps({ permissions: current })
    expect(wrapper.find('.empty-allowlist-warning').exists()).toBe(false)

    i18n.global.locale.value = 'en'
    await nextTick()
    expect(wrapper.text()).toContain('Selected fields')
    expect((wrapper.findAll('.field-permission-columns input')[0].element as HTMLInputElement).checked).toBe(true)
  })

  it('does not offer field restrictions for wildcard permissions', () => {
    const wrapper = mount(PermissionEditor, {
      props: { permissions: permissions('*', '*'), schemas: [{ name: 'public', resources: [books] }] },
      global: { plugins: [createVelmirI18n()] },
    })
    expect(wrapper.find('.field-permission-editor').exists()).toBe(false)
    expect(wrapper.text()).toContain('Wildcard *.*')
  })
})
