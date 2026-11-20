// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import ManyToManyPanel from './ManyToManyPanel.vue'
import { authState, handleUnauthorized } from './auth'
import { createVelmirI18n } from './i18n'
import type { ManyToManyRelation } from './manyToMany'
import type { RecordRow } from './records'
import type { ColumnMetadata, ResourceMetadata } from './schema'

function column(name: string, dataType = 'text', nullable = false): ColumnMetadata {
  return {
    name, data_type: dataType, nullable, default: null, primary_key: false,
    identity: '', generated: false, editable: true, enum_values: [],
  }
}

const parent: ResourceMetadata = {
  name: 'parents', kind: 'table', primary_key: ['id'], foreign_keys: [],
  columns: [column('id', 'bigint'), column('name')],
}
const target: ResourceMetadata = {
  name: 'targets', kind: 'table', primary_key: ['id'], foreign_keys: [],
  columns: [column('id', 'bigint'), column('name')],
}
const junction: ResourceMetadata = {
  name: 'parent_targets', kind: 'table', primary_key: ['parent_id', 'target_id'],
  foreign_keys: [
    { name: 'links_parent_fk', columns: ['parent_id'], referenced_schema: 'public', referenced_table: 'parents', referenced_columns: ['id'] },
    { name: 'links_target_fk', columns: ['target_id'], referenced_schema: 'public', referenced_table: 'targets', referenced_columns: ['id'] },
  ],
  columns: [column('parent_id', 'bigint'), column('target_id', 'bigint')],
}
const schemas = [{ name: 'public', resources: [parent, target, junction] }]
const relation: ManyToManyRelation = {
  id: 'public.parent_targets.links_parent_fk.links_target_fk',
  junctionSchema: 'public',
  junctionResource: junction,
  parentForeignKey: junction.foreign_keys[0],
  targetForeignKey: junction.foreign_keys[1],
  targetResource: target,
}
const parentRecord = { id: '9223372036854775807', name: 'Unchanged parent' }
const wrappers: VueWrapper[] = []

function recordsResponse(url: string, records: RecordRow[], hasNext = false): Response {
  const query = new URL(url, 'http://localhost').searchParams
  return new Response(JSON.stringify({
    records,
    page: Number(query.get('page')),
    limit: Number(query.get('limit')),
    has_next: hasNext,
  }), { status: 200 })
}

function defaultHandler(input: RequestInfo | URL, init?: RequestInit): Promise<Response> | Response {
  const url = String(input)
  if (url.includes('/parent_targets/records?')) {
    const page = Number(new URL(url, 'http://localhost').searchParams.get('page'))
    return recordsResponse(url, [{ parent_id: parentRecord.id, target_id: String(page) }], page === 1)
  }
  if (url.includes('/parent_targets/relations/resolve')) {
    const body = JSON.parse(String(init?.body))
    return new Response(JSON.stringify({
      relations: [{ name: 'links_target_fk', records: body.relations[0].keys.map((key: RecordRow) => ({ ...key, name: `Target ${key.id}` })) }],
    }), { status: 200 })
  }
  if (url.includes('/targets/records?')) {
    return recordsResponse(url, [{ id: '1', name: 'Linked target' }, { id: '2', name: 'Available target' }])
  }
  throw new Error(`Unexpected request: ${init?.method ?? 'GET'} ${url}`)
}

function mountPanel(
  handler: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response> | Response = defaultHandler,
  propOverrides: Record<string, unknown> = {},
) {
  const fetchMock = vi.fn().mockImplementation(handler)
  vi.stubGlobal('fetch', fetchMock)
  const i18n = createVelmirI18n()
  const wrapper = mount(ManyToManyPanel, {
    attachTo: document.body,
    props: {
      backendBaseURL: '',
      parentName: 'public.parents',
      relation,
      schemas,
      parentRecord,
      writeEnabled: true,
      ...propOverrides,
    },
    global: { plugins: [i18n] },
  })
  wrappers.push(wrapper)
  return { wrapper, fetchMock, i18n }
}

function mutationCalls(fetchMock: ReturnType<typeof vi.fn>, method: string) {
  return fetchMock.mock.calls.filter(([, init]) => init?.method === method)
}

beforeEach(() => {
  authState.status = 'authenticated'
  authState.permissions = [{ schema: '*', resource: '*', operations: ['read', 'create', 'delete'] }]
  window.localStorage.clear()
})

afterEach(() => {
  while (wrappers.length) wrappers.pop()?.unmount()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  handleUnauthorized()
  document.body.innerHTML = ''
  window.innerWidth = 1024
})

describe('many-to-many panel reads', () => {
  it('uses typed parent filters, one batched label request and independent pagination', async () => {
    const handler = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes('/parent_targets/records?')) {
        const page = Number(new URL(url, 'http://localhost').searchParams.get('page'))
        return recordsResponse(url, page === 1
          ? [{ parent_id: parentRecord.id, target_id: '1' }, { parent_id: parentRecord.id, target_id: '2' }]
          : [{ parent_id: parentRecord.id, target_id: '3' }], page === 1)
      }
      return defaultHandler(input, init)
    })
    const { wrapper, fetchMock, i18n } = mountPanel(handler)
    await flushPromises()

    const junctionURL = String(fetchMock.mock.calls.find(([url]) => String(url).includes('/parent_targets/records?'))![0])
    const query = new URL(junctionURL, 'http://localhost').searchParams
    expect(JSON.parse(query.get('filter')!)).toEqual([
      { column: 'parent_id', operator: 'eq', value: '9223372036854775807' },
    ])
    const resolveCalls = fetchMock.mock.calls.filter(([url]) => String(url).includes('/relations/resolve'))
    expect(resolveCalls).toHaveLength(1)
    expect(JSON.parse(String(resolveCalls[0][1]?.body))).toEqual({
      relations: [{ name: 'links_target_fk', keys: [{ id: '1' }, { id: '2' }] }],
    })
    expect(wrapper.text()).toContain('Target 1')
    expect(wrapper.text()).toContain('Target 2')
    expect(wrapper.text()).toContain('id=1')
    expect(wrapper.findAll('.many-to-many-records li')).toHaveLength(2)
    expect(fetchMock.mock.calls.filter(([url]) => String(url).includes('/targets/records?') && String(url).includes('filter='))).toHaveLength(0)

    await wrapper.get('[aria-label="Следующая страница связанных записей public.targets"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Target 3')
    expect(wrapper.text()).toContain('Страница 2')
    i18n.global.locale.value = 'en'
    await nextTick()
    expect(wrapper.text()).toContain('Page 2')
    expect(wrapper.text()).toContain('Target 3')
  })

  it('does not query for an unsaved or incomplete parent', async () => {
    const { wrapper, fetchMock } = mountPanel(defaultHandler, { parentRecord: undefined })
    await flushPromises()
    expect(fetchMock).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Сначала сохраните запись')
    expect(wrapper.find('.many-to-many-add').exists()).toBe(false)
  })

  it('keeps raw keys available when batch label resolution fails', async () => {
    const { wrapper } = mountPanel((input, init) => {
      if (String(input).includes('/relations/resolve')) {
        return new Response(JSON.stringify({ error: { code: 'RELATION_RESOLVE_FAILED', message: 'Failed' } }), { status: 500 })
      }
      return defaultHandler(input, init)
    })
    await flushPromises()
    expect(wrapper.text()).toContain('id=1')
    expect(wrapper.get('.many-to-many-label-warning').text()).toContain('исходное значение')
    expect(wrapper.find('.reverse-relation-error').exists()).toBe(false)
  })
})

describe('many-to-many permissions and relation creation', () => {
  it.each([
    [['read', 'create'] as const, true, true, true, false],
    [['read', 'delete'] as const, true, true, false, true],
    [['read', 'create', 'delete'] as const, false, true, false, false],
    [['read'] as const, true, true, false, false],
  ])('applies junction operations %s and writer=%s', async (operations, writeEnabled, _read, addVisible, removeVisible) => {
    authState.permissions = [
      { schema: 'public', resource: 'parents', operations: ['read'] },
      { schema: 'public', resource: 'targets', operations: ['read'] },
      { schema: 'public', resource: 'parent_targets', operations: [...operations] },
    ]
    const { wrapper } = mountPanel(defaultHandler, { writeEnabled })
    await flushPromises()
    expect(wrapper.find('.many-to-many-add').exists()).toBe(addVisible)
    expect(wrapper.find('.many-to-many-remove').exists()).toBe(removeVisible)
  })

  it('adds one relation with a flat junction POST, refreshes labels and never mutates parent/target', async () => {
    let linked = false
    const { wrapper, fetchMock } = mountPanel((input, init) => {
      const url = String(input)
      if (init?.method === 'POST' && url.endsWith('/parent_targets/records')) {
        linked = true
        return new Response(JSON.stringify({ record: { parent_id: parentRecord.id, target_id: '2' } }), { status: 201 })
      }
      if (url.includes('/parent_targets/records?')) {
        return recordsResponse(url, linked
          ? [{ parent_id: parentRecord.id, target_id: '1' }, { parent_id: parentRecord.id, target_id: '2' }]
          : [{ parent_id: parentRecord.id, target_id: '1' }])
      }
      return defaultHandler(input, init)
    })
    await flushPromises()
    await wrapper.get('[role="combobox"]').trigger('focus')
    const option = Array.from(document.querySelectorAll<HTMLButtonElement>('.relation-option')).find((item) => item.textContent?.includes('Available target'))!
    expect(option.disabled).toBe(false)
    option.click()
    await nextTick()
    const add = wrapper.get('.many-to-many-add > .primary-button')
    expect(add.attributes('disabled')).toBeUndefined()
    await add.trigger('click')
    await flushPromises()

    const posts = mutationCalls(fetchMock, 'POST').filter(([url]) => String(url).endsWith('/parent_targets/records'))
    expect(posts).toHaveLength(1)
    expect(JSON.parse(String(posts[0][1]?.body))).toEqual({
      values: { parent_id: '9223372036854775807', target_id: '2' },
    })
    expect(posts[0][1]?.headers).toHaveProperty('X-CSRF-Token')
    expect(wrapper.text()).toContain('Связь успешно добавлена')
    expect(wrapper.text()).toContain('Target 2')
    expect((wrapper.get('[role="combobox"]').element as HTMLInputElement).value).toBe('')
    expect(mutationCalls(fetchMock, 'PATCH')).toHaveLength(0)
    expect(mutationCalls(fetchMock, 'DELETE')).toHaveLength(0)
  })

  it('disables linked options, skips them with keyboard and handles duplicate conflict without retry', async () => {
    const { wrapper, fetchMock } = mountPanel((input, init) => {
      if (init?.method === 'POST' && String(input).endsWith('/parent_targets/records')) {
        return new Response(JSON.stringify({ error: { code: 'UNIQUE_VIOLATION', message: 'Duplicate' } }), { status: 409 })
      }
      return defaultHandler(input, init)
    })
    await flushPromises()
    const input = wrapper.get('[role="combobox"]')
    await input.trigger('focus')
    const options = Array.from(document.querySelectorAll<HTMLButtonElement>('.relation-option'))
    expect(options[0].disabled).toBe(true)
    expect(options[0].getAttribute('aria-disabled')).toBe('true')
    await input.trigger('keydown', { key: 'ArrowDown' })
    expect(input.attributes('aria-activedescendant')).toContain('-option-1')
    await input.trigger('keydown', { key: 'Enter' })
    await nextTick()
    await wrapper.get('.many-to-many-add > .primary-button').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Такая связь уже существует')
    expect(mutationCalls(fetchMock, 'POST').filter(([url]) => String(url).endsWith('/parent_targets/records'))).toHaveLength(1)
    expect((input.element as HTMLInputElement).value).toContain('Available target')
  })

  it('refreshes after committed create audit failure without automatic retry', async () => {
    const { wrapper, fetchMock } = mountPanel((input, init) => {
      if (init?.method === 'POST' && String(input).endsWith('/parent_targets/records')) {
        return new Response(JSON.stringify({
          error: { code: 'AUDIT_WRITE_FAILED', message: 'Audit failed', mutation_committed: true },
          record: { parent_id: parentRecord.id, target_id: '2' },
        }), { status: 500 })
      }
      return defaultHandler(input, init)
    })
    await flushPromises()
    await wrapper.get('[role="combobox"]').trigger('focus')
    Array.from(document.querySelectorAll<HTMLButtonElement>('.relation-option'))[1].click()
    await nextTick()
    await wrapper.get('.many-to-many-add > .primary-button').trigger('click')
    await flushPromises()

    expect(wrapper.get('.audit-warning').text()).toContain('Связь создана, но audit create не записан')
    expect(mutationCalls(fetchMock, 'POST').filter(([url]) => String(url).endsWith('/parent_targets/records'))).toHaveLength(1)
    expect(fetchMock.mock.calls.filter(([url]) => String(url).includes('/parent_targets/records?'))).toHaveLength(2)
  })
})

describe('many-to-many relation removal', () => {
  it('uses the full composite junction key, preserves parent/target and supports Escape/focus return', async () => {
    let deleted = false
    const { wrapper, fetchMock } = mountPanel((input, init) => {
      const url = String(input)
      if (init?.method === 'DELETE') {
        deleted = true
        return new Response(null, { status: 204 })
      }
      if (url.includes('/parent_targets/records?')) {
        return recordsResponse(url, deleted ? [] : [{ parent_id: parentRecord.id, target_id: '1' }])
      }
      return defaultHandler(input, init)
    })
    await flushPromises()
    const trigger = wrapper.get('.many-to-many-remove')
    await trigger.trigger('click')
    await nextTick()
    const dialog = document.querySelector<HTMLElement>('[role="dialog"]')!
    expect(dialog.getAttribute('aria-label')).toBe('Подтверждение удаления только связи')
    expect(dialog.textContent).toContain('Parent-запись и target-запись останутся')
    expect(dialog.textContent).toContain('{"parent_id":"9223372036854775807","target_id":"1"}')
    expect(dialog.textContent).toContain('public.parent_targets')
    expect(document.activeElement?.textContent).toBe('Отмена')

    dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await nextTick()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.activeElement).toBe(trigger.element)
    expect(mutationCalls(fetchMock, 'DELETE')).toHaveLength(0)

    await trigger.trigger('click')
    await nextTick()
    const confirm = document.querySelector<HTMLButtonElement>('.relation-remove-dialog .danger-button')!
    confirm.click()
    confirm.click()
    await flushPromises()
    const deletes = mutationCalls(fetchMock, 'DELETE')
    expect(deletes).toHaveLength(1)
    expect(String(deletes[0][0])).toContain(`/parent_targets/records/${encodeURIComponent('{"parent_id":"9223372036854775807","target_id":"1"}')}`)
    expect(wrapper.text()).toContain('Связь успешно удалена')
    expect(wrapper.text()).toContain('Связанных записей нет')
    expect(mutationCalls(fetchMock, 'PATCH')).toHaveLength(0)
    expect(mutationCalls(fetchMock, 'POST').filter(([url]) => String(url).endsWith('/parents/records') || String(url).endsWith('/targets/records'))).toHaveLength(0)
  })

  it('returns to the previous page after removing its last relation', async () => {
    const { wrapper, fetchMock } = mountPanel((input, init) => {
      const url = String(input)
      if (init?.method === 'DELETE') return new Response(null, { status: 204 })
      if (url.includes('/parent_targets/records?')) {
        const page = Number(new URL(url, 'http://localhost').searchParams.get('page'))
        return recordsResponse(url, [{ parent_id: parentRecord.id, target_id: String(page) }], page === 1)
      }
      return defaultHandler(input, init)
    })
    await flushPromises()
    await wrapper.get('[aria-label="Следующая страница связанных записей public.targets"]').trigger('click')
    await flushPromises()
    await wrapper.get('.many-to-many-remove').trigger('click')
    await nextTick()
    document.querySelector<HTMLButtonElement>('.relation-remove-dialog .danger-button')!.click()
    await flushPromises()

    const junctionReads = fetchMock.mock.calls.filter(([url]) => String(url).includes('/parent_targets/records?'))
    expect(new URL(String(junctionReads.at(-1)![0]), 'http://localhost').searchParams.get('page')).toBe('1')
    expect(wrapper.text()).toContain('Страница 1')
  })

  it('shows a safe constraint error and never retries committed delete audit failure', async () => {
    let failure: 'constraint' | 'audit' = 'constraint'
    const { wrapper, fetchMock } = mountPanel((input, init) => {
      if (init?.method === 'DELETE') {
        return new Response(JSON.stringify({
          error: failure === 'constraint'
            ? { code: 'FOREIGN_KEY_VIOLATION', message: 'Constraint SQL must not be rendered' }
            : { code: 'AUDIT_WRITE_FAILED', message: 'Audit failed', mutation_committed: true },
        }), { status: failure === 'constraint' ? 400 : 500 })
      }
      return defaultHandler(input, init)
    })
    await flushPromises()
    await wrapper.get('.many-to-many-remove').trigger('click')
    await nextTick()
    document.querySelector<HTMLButtonElement>('.relation-remove-dialog .danger-button')!.click()
    await flushPromises()
    expect(document.querySelector('.relation-remove-dialog .delete-error')?.textContent).toContain('ограничения целостности')
    expect(document.body.textContent).not.toContain('SQL must not')
    expect(mutationCalls(fetchMock, 'DELETE')).toHaveLength(1)

    failure = 'audit'
    document.querySelector<HTMLButtonElement>('.relation-remove-dialog .danger-button')!.click()
    await flushPromises()
    expect(wrapper.get('.audit-warning').text()).toContain('Связь удалена, но audit delete мог не записаться')
    expect(mutationCalls(fetchMock, 'DELETE')).toHaveLength(2)
  })
})

describe('many-to-many selection, locale and accessibility', () => {
  it('preserves selector selection and pagination across ru/en and keeps mobile dropdown in the viewport', async () => {
    window.innerWidth = 390
    const { wrapper, i18n } = mountPanel(defaultHandler)
    await flushPromises()
    await wrapper.get('[aria-label="Следующая страница связанных записей public.targets"]').trigger('click')
    await flushPromises()
    const input = wrapper.get('[role="combobox"]')
    await input.trigger('focus')
    document.querySelectorAll<HTMLButtonElement>('.relation-option')[0].click()
    await nextTick()
    const selected = (input.element as HTMLInputElement).value
    i18n.global.locale.value = 'en'
    await nextTick()

    expect((input.element as HTMLInputElement).value).toBe(selected)
    expect(wrapper.text()).toContain('Page 2')
    expect(input.attributes('aria-label')).toContain('search and select')
    await input.trigger('focus')
    await nextTick()
    const dropdown = document.querySelector<HTMLElement>('.relation-dropdown')!
    expect(dropdown.parentElement).toBe(document.body)
    const left = Number.parseFloat(dropdown.style.left)
    const width = Number.parseFloat(dropdown.style.width)
    expect(left).toBeGreaterThanOrEqual(8)
    expect(left + width).toBeLessThanOrEqual(382)
    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.get('section').attributes('aria-label')).toContain('Related public.targets')
  })
})
