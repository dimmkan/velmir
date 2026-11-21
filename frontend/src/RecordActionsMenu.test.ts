// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import RecordActionsMenu from './RecordActionsMenu.vue'
import { createVelmirI18n } from './i18n'

const wrappers: VueWrapper[] = []

function mountMenu(props = {}) {
  const wrapper = mount(RecordActionsMenu, {
    attachTo: document.body,
    props: { canEdit: true, canView: true, canHistory: true, canDelete: true, ...props },
    global: { plugins: [createVelmirI18n()] },
  })
  wrappers.push(wrapper)
  return wrapper
}

function items(): HTMLButtonElement[] {
  return Array.from(document.querySelectorAll<HTMLButtonElement>('[role="menuitem"]'))
}

function menu(): HTMLElement | null {
  return document.querySelector('[role="menu"]')
}

afterEach(() => {
  while (wrappers.length) wrappers.pop()?.unmount()
  document.body.innerHTML = ''
})

describe('record actions menu', () => {
  it('opens, navigates by keyboard, closes on Escape and restores focus', async () => {
    const wrapper = mountMenu()
    const trigger = wrapper.get('.record-actions-trigger')
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    expect(items().map((item) => item.textContent)).toEqual(['Редактировать', 'История', 'Удалить'])
    expect(document.activeElement).toBe(items()[0])
    menu()!.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
    expect(document.activeElement).toBe(items()[1])
    menu()!.dispatchEvent(new KeyboardEvent('keydown', { key: 'End', bubbles: true }))
    expect(document.activeElement).toBe(items()[2])
    menu()!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await nextTick()
    expect(menu()).toBeNull()
    expect(document.activeElement).toBe(trigger.element)
  })

  it('offers only permitted actions and emits one selected action', async () => {
    const wrapper = mountMenu({ canEdit: false, canView: true, canHistory: false, canDelete: true })
    await wrapper.get('.record-actions-trigger').trigger('click')
    await flushPromises()
    expect(items().map((item) => item.textContent)).toEqual(['Просмотр', 'Удалить'])
    items()[0].click()
    await flushPromises()
    expect(wrapper.emitted('edit')).toHaveLength(1)
    expect(wrapper.emitted('delete')).toBeUndefined()
    expect(menu()).toBeNull()
  })

  it('shows a neutral placeholder when no action is available', () => {
    const wrapper = mountMenu({ canEdit: false, canView: false, canHistory: false, canDelete: false })
    expect(wrapper.find('.record-actions-trigger').exists()).toBe(false)
    expect(wrapper.get('.record-actions-empty').attributes('aria-label')).toBe('Нет доступных действий')
  })

  it('closes on outside pointer and when another record menu opens', async () => {
    const first = mountMenu()
    const second = mountMenu()
    await first.get('.record-actions-trigger').trigger('click')
    await flushPromises()
    expect(menu()).not.toBeNull()
    await second.get('.record-actions-trigger').trigger('click')
    await flushPromises()
    expect(first.get('.record-actions-trigger').attributes('aria-expanded')).toBe('false')
    expect(second.get('.record-actions-trigger').attributes('aria-expanded')).toBe('true')
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await nextTick()
    expect(menu()).toBeNull()
  })
})
