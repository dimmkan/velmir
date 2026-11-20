// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import ReverseRelationPanel from './ReverseRelationPanel.vue'
import { authState, handleUnauthorized } from './auth'
import { createVelmirI18n } from './i18n'
import type { RecordRow } from './records'
import type { ColumnMetadata, ResourceMetadata, ReverseRelationMetadata } from './schema'

function column(name: string, dataType = 'text'): ColumnMetadata {
  return {
    name, data_type: dataType, nullable: true, default: null, primary_key: name === 'id',
    identity: '', generated: false, editable: true, enum_values: [],
  }
}

const childResource: ResourceMetadata = {
  name: 'children', kind: 'table', primary_key: ['id'], foreign_keys: [],
  columns: [column('id', 'bigint'), column('tenant_id', 'bigint'), column('parent_id', 'uuid'), column('title')],
}
const relation: ReverseRelationMetadata = {
  name: 'children_parent_fk', schema: 'public', resource: 'children',
  columns: ['tenant_id', 'parent_id'], referenced_columns: ['tenant_id', 'external_id'],
}
const parentRecord: RecordRow = {
  tenant_id: '9223372036854775807', external_id: '00112233-4455-6677-8899-aabbccddeeff', name: 'Parent',
}
let wrapper: VueWrapper | null = null

function response(url: string, records: RecordRow[], hasNext: boolean): Response {
  const query = new URL(url, 'http://localhost').searchParams
  return new Response(JSON.stringify({
    records, page: Number(query.get('page')), limit: Number(query.get('limit')), has_next: hasNext,
  }), { status: 200 })
}

function mountPanel(parent?: RecordRow) {
  const fetchMock = vi.fn().mockImplementation(async (input: RequestInfo | URL) => {
    const url = String(input)
    const page = Number(new URL(url, 'http://localhost').searchParams.get('page'))
    return response(url, [{ id: String(page), title: `Child ${page}` }], page === 1)
  })
  vi.stubGlobal('fetch', fetchMock)
  const i18n = createVelmirI18n()
  wrapper = mount(ReverseRelationPanel, {
    attachTo: document.body,
    props: { backendBaseURL: '', relation, childResource, parentRecord: parent },
    global: { plugins: [i18n] },
  })
  return { wrapper, fetchMock, i18n }
}

beforeEach(() => {
  authState.status = 'authenticated'
  authState.permissions = [{ schema: '*', resource: '*', operations: ['read'] }]
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.unstubAllGlobals()
  handleUnauthorized()
  document.body.innerHTML = ''
})

describe('ReverseRelationPanel', () => {
  it('uses all typed composite parent filters and independent pagination', async () => {
    const { wrapper, fetchMock, i18n } = mountPanel(parentRecord)
    await flushPromises()
    expect(wrapper.text()).toContain('Child 1')
    const firstQuery = new URL(String(fetchMock.mock.calls[0][0]), 'http://localhost').searchParams
    expect(JSON.parse(firstQuery.get('filter')!)).toEqual([
      { column: 'tenant_id', operator: 'eq', value: '9223372036854775807' },
      { column: 'parent_id', operator: 'eq', value: '00112233-4455-6677-8899-aabbccddeeff' },
    ])
    expect(firstQuery.get('limit')).toBe('5')

    await wrapper.get('[aria-label="Следующая страница дочерних записей children"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Child 2')
    const secondQuery = new URL(String(fetchMock.mock.calls[1][0]), 'http://localhost').searchParams
    expect(secondQuery.get('page')).toBe('2')
    expect(JSON.parse(secondQuery.get('filter')!)).toEqual(JSON.parse(firstQuery.get('filter')!))

    i18n.global.locale.value = 'en'
    await nextTick()
    expect(wrapper.text()).toContain('Page 2')
    expect(wrapper.text()).toContain('Child 2')
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(wrapper.find('input').exists()).toBe(false)
    expect(wrapper.find('form').exists()).toBe(false)
  })

  it('does not query without a saved complete parent key', async () => {
    const { wrapper, fetchMock } = mountPanel(undefined)
    await flushPromises()
    expect(fetchMock).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Нет связанной записи')
  })

  it.each([
    [401, 'UNAUTHENTICATED', 'Требуется аутентификация'],
    [403, 'FORBIDDEN', 'У вас нет права'],
    [500, 'RECORDS_QUERY_FAILED', 'Не удалось загрузить дочерние записи'],
  ])('shows isolated HTTP %s state with retry', async (status, code, message) => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      error: { code, message: 'Server error' },
    }), { status }))
    vi.stubGlobal('fetch', fetchMock)
    wrapper = mount(ReverseRelationPanel, {
      props: { backendBaseURL: '', relation, childResource, parentRecord },
      global: { plugins: [createVelmirI18n()] },
    })
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain(message)
    expect(wrapper.get('[role="alert"] button').text()).toBe('Повторить')
    if (status === 403) expect(authState.status).toBe('authenticated')
  })

  it('has accessible labels and overflow-safe card markup instead of a wide table', async () => {
    const { wrapper } = mountPanel(parentRecord)
    await flushPromises()
    expect(wrapper.get('section').attributes('aria-label')).toBe('Дочерние записи ресурса children')
    expect(wrapper.get('footer').attributes('aria-label')).toBe('Пагинация дочерних записей children')
    expect(wrapper.get('li').attributes('aria-label')).toContain('Дочерняя запись id=1')
    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.find('.reverse-relation-records dl').exists()).toBe(true)
    expect(wrapper.find('.reverse-relation-pagination').exists()).toBe(true)
  })
})
