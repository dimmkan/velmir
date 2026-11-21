<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { LOCALE_STORAGE_KEY, type Locale } from './i18n'
import type { DatabaseState, ReadinessResponse } from './readiness'

const props = defineProps<{
  viewState?: 'loading' | 'ready' | 'error'
  readiness?: ReadinessResponse | null
}>()
const emit = defineEmits<{ retry: [] }>()
const { locale, t } = useI18n()

function selectLocale(next: Locale) {
  locale.value = next
  try {
    window.localStorage.setItem(LOCALE_STORAGE_KEY, next)
  } catch {
    return
  }
}

function databaseStateLabel(state: DatabaseState | undefined) {
  if (props.viewState === 'loading') return t('readiness.checking')
  const keys: Record<DatabaseState, string> = {
    ready: 'readiness.ready',
    unavailable: 'readiness.unavailable',
    not_configured: 'readiness.notConfigured',
  }
  return state ? t(keys[state]) : t('common.unknown')
}
</script>

<template>
  <footer class="app-footer">
    <span class="footer-product">{{ t('footer.product') }}</span>
    <section v-if="viewState" class="footer-readiness" :aria-label="t('readiness.label')">
      <div>
        <span>{{ t('readiness.backend') }}</span>
        <strong :class="`status-${viewState === 'error' ? 'unavailable' : viewState}`">
          {{ viewState === 'loading' ? t('readiness.checking') : viewState === 'error' ? t('readiness.unavailable') : t('readiness.available') }}
        </strong>
      </div>
      <div>
        <span>{{ t('readiness.adminDatabase') }}</span>
        <strong :class="`status-${readiness?.admin_database ?? viewState}`">{{ databaseStateLabel(readiness?.admin_database) }}</strong>
      </div>
      <div>
        <span>{{ t('readiness.targetDatabase') }}</span>
        <strong :class="`status-${readiness?.target_database ?? viewState}`">{{ databaseStateLabel(readiness?.target_database) }}</strong>
      </div>
      <button v-if="viewState === 'error'" type="button" class="footer-retry" @click="emit('retry')">{{ t('readiness.retry') }}</button>
    </section>
    <div class="language-switch" role="group" :aria-label="t('language.label')">
      <button
        type="button"
        :class="{ active: locale === 'ru' }"
        :aria-label="t('language.switchToRussian')"
        :aria-pressed="locale === 'ru'"
        @click="selectLocale('ru')"
      >{{ t('language.russian') }}</button>
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
