<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { LOCALE_STORAGE_KEY, type Locale } from './i18n'

const { locale, t } = useI18n()

function selectLocale(next: Locale) {
  locale.value = next
  try {
    window.localStorage.setItem(LOCALE_STORAGE_KEY, next)
  } catch {
    return
  }
}
</script>

<template>
  <footer class="app-footer">
    <span>{{ t('footer.product') }}</span>
    <div class="language-switch" role="group" :aria-label="t('language.label')">
      <button
        type="button"
        :class="{ active: locale === 'ru' }"
        :aria-label="t('language.switchToRussian')"
        :aria-pressed="locale === 'ru'"
        @click="selectLocale('ru')"
      >{{ t('language.russian') }}</button>
      <span aria-hidden="true">|</span>
      <button
        type="button"
        :class="{ active: locale === 'en' }"
        :aria-label="t('language.switchToEnglish')"
        :aria-pressed="locale === 'en'"
        @click="selectLocale('en')"
      >{{ t('language.english') }}</button>
    </div>
  </footer>
</template>
