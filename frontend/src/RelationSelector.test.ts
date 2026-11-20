// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import RelationSelector from './RelationSelector.vue'
import { createVelmirI18n } from './i18n'
import type { RecordRow } from './records'
import type { ColumnMetadata, ResourceMetadata } from './schema'

function column(name: string, dataType: string): ColumnMetadata {
  return {
    name,
    data_type: dataType,
    nullable: false,
    default: null,
    primary_key: name === 'id',
    identity: '',
    generated: false,
    editable: true,
    enum_values: [],
  }
}

const resource: ResourceMetadata = {
  name: 'parents',
  kind: 'table',
  primary_key: ['id'],
  foreign_keys: [],
  columns: [column('id', 'bigint'), column('name', 'text'), column('description', 'text')],
}

let wrapper: VueWrapper | null = null

function response(records: RecordRow[], url: string, hasNext = false): Response {
  const parsed = new URL(url, 'http://localhost')
  return new Response(JSON.stringify({
    records,
    page: Number(parsed.searchParams.get('page')),
    limit: Number(parsed.searchParams.get('limit')),
    has_next: hasNext,
  }), { status: 200 })
}

function mountSelector(
  fetchImplementation: (url: string) => Promise<Response>,
  propOverrides: Record<string, unknown> = {},
) {
  const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => fetchImplementation(String(input)))
  vi.stubGlobal('fetch', fetchMock)
  const i18n = createVelmirI18n()
  wrapper = mount(RelationSelector, {
    attachTo: document.body,
    props: {
      backendBaseURL: '',
      schemaName: 'public',
      resource,
      referencedColumns: ['id'],
      selectedValues: [],
      hasSelection: false,
      inputId: 'field-parent_id',
      label: 'parent_id',
      nullable: true,
      required: false,
      disabled: false,
      ...propOverrides,
    },
    global: { plugins: [i18n] },
  })
  return { wrapper, fetchMock, i18n }
}

function options(): HTMLButtonElement[] {
  return Array.from(document.querySelectorAll<HTMLButtonElement>('.relation-option'))
}

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.useRealTimers()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  window.localStorage.clear()
  document.body.innerHTML = ''
})

describe('relation selector options and requests', () => {
  it('loads simple FK options and distinguishes identical labels by referenced keys', async () => {
    const { wrapper } = mountSelector(async (url) => response([
      { id: '1', name: 'Same name' },
      { id: '2', name: 'Same name' },
    ], url))
    await flushPromises()
    await wrapper.get('[role="combobox"]').trigger('focus')

    expect(options()).toHaveLength(2)
    expect(options()[0].textContent).toContain('Same name')
    expect(options()[0].textContent).toContain('id=1')
    expect(options()[1].textContent).toContain('id=2')

    options()[0].click()
    await nextTick()
    expect(wrapper.emitted('select')?.[0]?.[0]).toEqual({ id: '1', name: 'Same name' })
  })

  it('loads an initial value outside the first page with exact typed filters', async () => {
    const selectedID = '9223372036854775807'
    const { wrapper, fetchMock } = mountSelector(async (url) => {
      const parsed = new URL(url, 'http://localhost')
      if (parsed.searchParams.has('filter')) return response([{ id: selectedID, name: 'Selected parent' }], url)
      return response([{ id: '1', name: 'First page parent' }], url)
    }, { selectedValues: [selectedID], hasSelection: true })
    await flushPromises()

    expect((wrapper.get('[role="combobox"]').element as HTMLInputElement).value).toContain('Selected parent')
    expect((wrapper.get('[role="combobox"]').element as HTMLInputElement).value).toContain(selectedID)
    const selectedRequest = fetchMock.mock.calls.map(([url]) => String(url)).find((url) => url.includes('filter='))
    const query = new URL(selectedRequest!, 'http://localhost').searchParams
    expect(query.get('limit')).toBe('1')
    expect(JSON.parse(query.get('filter')!)).toEqual([{ column: 'id', operator: 'eq', value: selectedID }])
  })

  it('keeps the current technical key when the selected record cannot be loaded', async () => {
    const { wrapper } = mountSelector(async (url) => {
      const parsed = new URL(url, 'http://localhost')
      return response(parsed.searchParams.has('filter') ? [] : [{ id: '1', name: 'First' }], url)
    }, { selectedValues: ['999'], hasSelection: true })
    await flushPromises()

    expect((wrapper.get('[role="combobox"]').element as HTMLInputElement).value).toBe('id=999')
    expect(wrapper.emitted('clear')).toBeUndefined()
  })

  it('debounces server-side search', async () => {
    vi.useFakeTimers()
    const { wrapper, fetchMock } = mountSelector(async (url) => response([], url))
    await flushPromises()
    const input = wrapper.get('[role="combobox"]')
    await input.trigger('focus')
    await input.setValue('search text')

    expect(fetchMock).toHaveBeenCalledTimes(1)
    vi.advanceTimersByTime(299)
    await nextTick()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    vi.advanceTimersByTime(1)
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(String(fetchMock.mock.calls[1][0])).toContain('search=search+text')
  })

  it('appends the next page with Load more', async () => {
    const { wrapper } = mountSelector(async (url) => {
      const page = Number(new URL(url, 'http://localhost').searchParams.get('page'))
      return response([{ id: String(page), name: `Parent ${page}` }], url, page === 1)
    })
    await flushPromises()
    await wrapper.get('[role="combobox"]').trigger('focus')
    document.querySelector<HTMLButtonElement>('.relation-load-more')!.click()
    await flushPromises()

    expect(options()).toHaveLength(2)
    expect(options().map((option) => option.textContent)).toEqual(expect.arrayContaining([
      expect.stringContaining('Parent 1'),
      expect.stringContaining('Parent 2'),
    ]))
  })
})

describe('relation selector state and accessibility', () => {
  it('clears a nullable relation without converting null to a string', async () => {
    const { wrapper } = mountSelector(async (url) => response([{ id: '1', name: 'Parent' }], url), {
      selectedValues: ['1'], hasSelection: true, nullable: true,
    })
    await flushPromises()
    await wrapper.get('.relation-clear').trigger('click')

    expect(wrapper.emitted('clear')).toHaveLength(1)
    expect(wrapper.emitted('select')).toBeUndefined()
  })

  it('does not expose clear for a required relation', async () => {
    const { wrapper } = mountSelector(async (url) => response([{ id: '1', name: 'Parent' }], url), {
      selectedValues: ['1'], hasSelection: true, nullable: false, required: true,
    })
    await flushPromises()

    expect(wrapper.find('.relation-clear').exists()).toBe(false)
    expect(wrapper.get('[role="combobox"]').attributes('aria-required')).toBe('true')
  })

  it('supports keyboard navigation, ARIA states and Escape without losing focus', async () => {
    const { wrapper } = mountSelector(async (url) => response([
      { id: '1', name: 'One' }, { id: '2', name: 'Two' },
    ], url))
    await flushPromises()
    const input = wrapper.get('[role="combobox"]')
    await input.trigger('focus')

    expect(input.attributes('aria-expanded')).toBe('true')
    await input.trigger('keydown', { key: 'ArrowDown' })
    expect(input.attributes('aria-activedescendant')).toBe('field-parent_id-listbox-option-0')
    await input.trigger('keydown', { key: 'Enter' })
    await nextTick()
    expect(wrapper.emitted('select')).toHaveLength(1)
    expect(input.attributes('aria-expanded')).toBe('false')

    await wrapper.setProps({ selectedValues: ['1'], hasSelection: true })
    await input.trigger('focus')
    expect(options()[0].getAttribute('aria-selected')).toBe('true')
    await input.trigger('keydown', { key: 'Escape' })

    expect(input.attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(input.element)
  })

  it('shows loading, error and empty states and allows retry', async () => {
    let reject = true
    const { wrapper } = mountSelector(async (url) => {
      if (reject) throw new Error('network')
      return response([], url)
    })
    await flushPromises()
    await wrapper.get('[role="combobox"]').trigger('focus')
    expect(document.body.textContent).toContain('Не удалось загрузить связанные записи')

    reject = false
    document.querySelector<HTMLButtonElement>('.relation-error button')!.click()
    await flushPromises()
    expect(document.body.textContent).toContain('Связанные записи не найдены')
  })

  it('does not allow changes when disabled', async () => {
    const { wrapper, fetchMock } = mountSelector(async (url) => response([{ id: '1', name: 'Parent' }], url), {
      disabled: true,
    })
    await flushPromises()
    const input = wrapper.get('[role="combobox"]')

    expect(input.attributes('disabled')).toBeDefined()
    expect(wrapper.find('.relation-clear').exists()).toBe(false)
    await input.trigger('keydown', { key: 'ArrowDown' })
    expect(wrapper.emitted('select')).toBeUndefined()
    expect(document.querySelector('.relation-dropdown')).toBeNull()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('preserves selection when switching ru/en', async () => {
    const { wrapper, i18n, fetchMock } = mountSelector(async (url) => response([
      { id: '1', name: 'Selected parent' },
    ], url), { selectedValues: ['1'], hasSelection: true })
    await flushPromises()
    const input = wrapper.get('[role="combobox"]')
    const before = (input.element as HTMLInputElement).value
    const requestCount = fetchMock.mock.calls.length

    i18n.global.locale.value = 'en'
    await nextTick()

    expect((input.element as HTMLInputElement).value).toBe(before)
    expect(input.attributes('placeholder')).toBe('Select a related record')
    expect(input.attributes('aria-label')).toContain('search and select')
    expect(fetchMock).toHaveBeenCalledTimes(requestCount)
  })
})
