// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import BaseModal from './BaseModal.vue'
import { createVelmirI18n } from './i18n'

let wrapper: ReturnType<typeof mount> | undefined

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
  document.body.style.overflow = ''
})

describe('BaseModal accessibility', () => {
  it('traps focus, closes on Escape and returns focus to the opener', async () => {
    const opener = document.createElement('button')
    document.body.append(opener)
    opener.focus()
    wrapper = mount(BaseModal, {
      attachTo: document.body,
      props: { title: 'Тестовое окно' },
      slots: {
        default: '<input aria-label="Поле" />',
        footer: '<button class="last-action">Готово</button>',
      },
      global: { plugins: [createVelmirI18n()], stubs: { teleport: true } },
    })
    await nextTick()
    await nextTick()
    const dialog = wrapper.get('[role="dialog"]')
    expect(dialog.attributes('aria-modal')).toBe('true')
    expect(dialog.attributes('aria-labelledby')).toBe(wrapper.get('h2').attributes('id'))
    expect(document.body.style.overflow).toBe('hidden')
    expect(document.activeElement).toBe(wrapper.get('.modal-close').element)

    await dialog.trigger('keydown', { key: 'Tab', shiftKey: true })
    expect(document.activeElement).toBe(wrapper.get('.last-action').element)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await nextTick()
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
    wrapper = undefined
    expect(document.activeElement).toBe(opener)
    expect(document.body.style.overflow).toBe('')
  })

  it('does not close pending mutation and closes by backdrop otherwise', async () => {
    wrapper = mount(BaseModal, {
      props: { title: 'Сохранение', pending: true },
      global: { plugins: [createVelmirI18n()], stubs: { teleport: true } },
    })
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await wrapper.get('.dialog-backdrop').trigger('mousedown')
    expect(wrapper.emitted('close')).toBeUndefined()
    await wrapper.setProps({ pending: false })
    await wrapper.get('.dialog-backdrop').trigger('mousedown')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
