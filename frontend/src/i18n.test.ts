// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import AppFooter from './AppFooter.vue'
import LoginPage from './LoginPage.vue'
import {
  createVelmirI18n,
  i18n,
  LOCALE_STORAGE_KEY,
  localizeError,
} from './i18n'
import en from './locales/en'
import ru from './locales/ru'

const LoginHost = defineComponent({
  components: { LoginPage, AppFooter },
  template: '<div><LoginPage :pending="false" error="" /><AppFooter /></div>',
})

function mountLogin() {
  const instance = createVelmirI18n()
  const wrapper = mount(LoginHost, { global: { plugins: [instance] } })
  return { wrapper, instance }
}

function translationPaths(value: unknown, prefix = ''): string[] {
  if (typeof value === 'string') return [prefix]
  if (typeof value !== 'object' || value === null) return []
  return Object.entries(value).flatMap(([key, child]) => translationPaths(child, prefix ? `${prefix}.${key}` : key))
}

beforeEach(() => {
  window.localStorage.clear()
  window.history.replaceState({}, '', '/login')
  i18n.global.locale.value = 'ru'
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  window.localStorage.clear()
  document.documentElement.lang = 'ru'
})

describe('locale selection', () => {
  it('uses Russian by default and switches the visible interface to English without a reload', async () => {
    const warning = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const { wrapper } = mountLogin()

    expect(wrapper.get('h1').text()).toBe('Войти')
    expect(wrapper.get('.login-logo').attributes('src')).toBe('/brand/velmir-logo-light.svg')
    expect(wrapper.get('.login-logo').attributes('alt')).toBe('Velmir')
    expect(wrapper.get('.app-footer').text()).toContain('Velmir · PostgreSQL Admin')
    expect(document.documentElement.lang).toBe('ru')

    const languageButtons = wrapper.findAll('.language-switch button')
    expect(languageButtons).toHaveLength(2)
    expect(languageButtons[0].attributes('aria-pressed')).toBe('true')
    expect(languageButtons[1].attributes('aria-pressed')).toBe('false')
    expect(languageButtons[0].attributes('aria-label')).toBeTruthy()
    expect(languageButtons[1].attributes('aria-label')).toBeTruthy()

    await languageButtons[1].trigger('click')

    expect(wrapper.get('h1').text()).toBe('Sign in')
    expect(wrapper.get('input[type="password"]')).toBeTruthy()
    expect(window.localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('en')
    expect(window.localStorage).toHaveLength(1)
    expect(document.documentElement.lang).toBe('en')
    expect(wrapper.findAll('.language-switch button')[1].attributes('aria-pressed')).toBe('true')
    expect(wrapper.text()).not.toMatch(/\b(auth|records|administration|validation|errors)\.[A-Za-z]/)
    expect(warning).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('restores an explicit English choice from velmir_locale', () => {
    window.localStorage.setItem(LOCALE_STORAGE_KEY, 'en')
    const { wrapper } = mountLogin()

    expect(wrapper.get('h1').text()).toBe('Sign in')
    expect(document.documentElement.lang).toBe('en')
    expect(window.localStorage).toHaveLength(1)
    wrapper.unmount()
  })

  it('falls back to Russian for an invalid stored value without persisting anything new', () => {
    window.localStorage.setItem(LOCALE_STORAGE_KEY, 'de')
    const { wrapper } = mountLogin()

    expect(wrapper.get('h1').text()).toBe('Войти')
    expect(window.localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('de')
    expect(window.localStorage).toHaveLength(1)
    wrapper.unmount()
  })
})

describe('translations and errors', () => {
  it('keeps the Russian and English namespaces structurally identical and non-empty', () => {
    const russianPaths = translationPaths(ru).sort()
    const englishPaths = translationPaths(en).sort()
    expect(englishPaths).toEqual(russianPaths)
    expect(russianPaths.length).toBeGreaterThan(100)
  })

  it('maps server codes, HTTP states and unknown errors to localized messages', () => {
    i18n.global.locale.value = 'ru'
    expect(localizeError('INVALID_CREDENTIALS', 401)).toBe('Неверный email или пароль.')
    expect(localizeError('FORBIDDEN', 403)).toBe('У вас нет права на выполнение этого действия.')
    expect(localizeError('UNLISTED_SERVER_CODE')).toBe(ru.errors.generic)

    i18n.global.locale.value = 'en'
    expect(localizeError('INVALID_CREDENTIALS')).toBe('Invalid email or password.')
    expect(localizeError('UNLISTED_SERVER_CODE')).toBe(en.errors.generic)
    expect(localizeError('UNAUTHENTICATED', 401)).toBe(en.errors.unauthenticated)
  })

  it('does not leave known hard-coded form strings in the localized components', () => {
    const sources = import.meta.glob('./*.vue', { query: '?raw', import: 'default', eager: true })
    for (const source of Object.values(sources)) {
      for (const text of [
        'Discard unsaved changes?',
        'Generated by the database',
        '>Cancel<',
        '>Create record<',
        '>Save changes<',
        '>Unsaved changes<',
        '>required<',
      ]) {
        expect(source).not.toContain(text)
      }
    }
  })
})
