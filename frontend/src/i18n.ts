import { createI18n } from 'vue-i18n'
import { watch } from 'vue'
import en from './locales/en'
import ru from './locales/ru'

export const LOCALES = ['ru', 'en'] as const
export type Locale = (typeof LOCALES)[number]
export const LOCALE_STORAGE_KEY = 'velmir_locale'

function isLocale(value: unknown): value is Locale {
  return typeof value === 'string' && LOCALES.includes(value as Locale)
}

export function storedLocale(): Locale {
  if (typeof window === 'undefined') return 'ru'
  try {
    const value = window.localStorage.getItem(LOCALE_STORAGE_KEY)
    return isLocale(value) ? value : 'ru'
  } catch {
    return 'ru'
  }
}

export function createVelmirI18n() {
  const instance = createI18n({
    legacy: false,
    locale: storedLocale(),
    fallbackLocale: 'en',
    messages: { ru, en },
    missingWarn: false,
    fallbackWarn: false,
  })
  watch(instance.global.locale, (locale) => {
    if (typeof document !== 'undefined') document.documentElement.lang = locale
  }, { immediate: true })
  return instance
}

export const i18n = createVelmirI18n()

export function translate(key: string): string {
  return i18n.global.t(key)
}

export function errorTranslationKey(code: string, status = 0): string {
  if (code === 'INVALID_CREDENTIALS') return 'errors.codes.INVALID_CREDENTIALS'
  if (status === 401) return 'errors.unauthenticated'
  if (status === 403 && code !== 'CSRF_TOKEN_INVALID' && code !== 'TARGET_READ_ONLY') {
    return 'errors.forbidden'
  }
  const key = `errors.codes.${code}`
  return i18n.global.te(key) ? key : 'errors.generic'
}

export function fieldErrorTranslationKey(code: string): string {
  const key = `errors.codes.${code}`
  return i18n.global.te(key) ? key : 'errors.invalidField'
}

export function localizeError(code: string, status = 0): string {
  return translate(errorTranslationKey(code, status))
}

export function localizeFieldError(code: string): string {
  return translate(fieldErrorTranslationKey(code))
}

export function setLocale(locale: Locale) {
  i18n.global.locale.value = locale
  try {
    window.localStorage.setItem(LOCALE_STORAGE_KEY, locale)
  } catch {
    return
  }
}
