<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { errorTranslationKey } from './i18n'
import {
  DEFAULT_HISTORY_LIMIT,
  fetchRecordHistory,
  historyChanges,
  HistoryRequestError,
  type HistoryEvent,
  type HistoryResponse,
} from './history'
import { formatRecordValue } from './records'
import type { ResourceMetadata } from './schema'

const props = defineProps<{
  backendBaseURL: string
  schemaName: string
  resource: ResourceMetadata
  recordKey: string
}>()
const emit = defineEmits<{ close: [] }>()
const { t, locale } = useI18n()

const result = ref<HistoryResponse | null>(null)
const page = ref(1)
const limit = ref(DEFAULT_HISTORY_LIMIT)
const loading = ref(false)
const errorMessage = ref('')
const errorStatus = ref(0)
const errorCode = ref('')
let controller: AbortController | null = null
let requestID = 0
let timeout = 0

async function loadHistory() {
  const currentRequest = ++requestID
  controller?.abort()
  controller = new AbortController()
  window.clearTimeout(timeout)
  timeout = window.setTimeout(() => controller?.abort(), 12000)
  loading.value = true
  result.value = null
  errorMessage.value = ''
  errorStatus.value = 0
  errorCode.value = ''

  try {
    const response = await fetchRecordHistory(
      props.backendBaseURL,
      props.schemaName,
      props.resource.name,
      props.recordKey,
      page.value,
      limit.value,
      controller.signal,
    )
    if (currentRequest === requestID) result.value = response
  } catch (error) {
    if (currentRequest !== requestID) return
    if (error instanceof HistoryRequestError) {
      errorMessage.value = errorTranslationKey(error.code, error.status)
      errorStatus.value = error.status
      errorCode.value = error.code
    } else if (error instanceof DOMException && error.name === 'AbortError') {
      errorMessage.value = 'history.requestCancelled'
    } else {
      errorMessage.value = 'errors.generic'
    }
  } finally {
    window.clearTimeout(timeout)
    if (currentRequest === requestID) loading.value = false
  }
}

function changePage(nextPage: number) {
  page.value = nextPage
  void loadHistory()
}

function changeLimit(event: Event) {
  limit.value = Number((event.target as HTMLSelectElement).value)
  page.value = 1
  void loadHistory()
}

function changes(event: HistoryEvent) {
  return historyChanges(event, props.resource)
}

watch(() => [props.schemaName, props.resource.name, props.recordKey], () => {
  page.value = 1
  void loadHistory()
}, { immediate: true })

onBeforeUnmount(() => {
  requestID++
  controller?.abort()
  window.clearTimeout(timeout)
})
</script>

<template>
  <section class="history-panel" :aria-label="t('history.title')" :aria-busy="loading">
    <div class="history-heading">
      <div>
        <p class="eyebrow">{{ t('history.title') }}</p>
        <h3>{{ resource.name }}</h3>
        <code class="history-record-key">{{ recordKey }}</code>
      </div>
      <button type="button" class="form-close" :aria-label="t('history.close')" @click="emit('close')">×</button>
    </div>

    <div v-if="loading" class="history-state" role="status">{{ t('history.loading') }}</div>
    <div v-else-if="errorMessage" class="history-state history-error" role="alert">
      <strong>{{ errorStatus === 403 ? t('history.forbidden') : t('history.loadFailed') }}</strong>
      <p>{{ t(errorMessage) }}</p>
      <code v-if="errorCode">{{ errorCode }}</code>
      <button type="button" class="secondary-button" @click="loadHistory">{{ t('common.retry') }}</button>
    </div>
    <div v-else-if="result?.events.length === 0" class="history-state">
      <strong>{{ t('history.empty') }}</strong>
      <p>{{ t('history.emptyHint') }}</p>
    </div>

    <div v-else-if="result" class="history-events">
      <article v-for="event in result.events" :key="event.id" class="history-event">
        <header class="history-event-header">
          <span class="history-action" :class="`history-action-${event.action}`">
            {{ t(event.action === 'create' ? 'history.created' : 'history.updated') }}
          </span>
          <dl class="history-event-meta">
            <div>
              <dt>{{ t('history.actor') }}</dt>
              <dd>{{ event.actor.display_name }} <span>({{ event.actor.email }})</span></dd>
            </div>
            <div>
              <dt>{{ t('history.timestamp') }}</dt>
              <dd>{{ formatRecordValue(event.timestamp, 'timestamp with time zone', locale) }}</dd>
            </div>
          </dl>
        </header>

        <h4>{{ t(event.action === 'create' ? 'history.createdValues' : 'history.changedFields') }}</h4>
        <p v-if="changes(event).length === 0" class="muted">{{ t('history.noVisibleChanges') }}</p>
        <dl v-else class="history-changes">
          <div
            v-for="change in changes(event)"
            :key="change.column.name"
            class="history-change"
            :aria-label="t('history.fieldChange', { field: change.column.name })"
          >
            <dt>
              <span>{{ change.column.name }}</span>
              <code>{{ change.column.data_type }}</code>
            </dt>
            <dd v-if="event.action === 'create'" class="history-value">
              <span class="history-value-label">{{ t('history.after') }}</span>
              <span class="history-value-content" :class="{ 'null-value': change.after === null }">{{ formatRecordValue(change.after, change.column.data_type, locale) }}</span>
            </dd>
            <dd v-else class="history-before-after">
              <span class="history-value">
                <span class="history-value-label">{{ t('history.before') }}</span>
                <span class="history-value-content" :class="{ 'null-value': change.before === null }">{{ formatRecordValue(change.before, change.column.data_type, locale) }}</span>
              </span>
              <span class="history-arrow" aria-hidden="true">→</span>
              <span class="history-value">
                <span class="history-value-label">{{ t('history.after') }}</span>
                <span class="history-value-content" :class="{ 'null-value': change.after === null }">{{ formatRecordValue(change.after, change.column.data_type, locale) }}</span>
              </span>
            </dd>
          </div>
        </dl>
      </article>
    </div>

    <footer v-if="result && !errorMessage" class="pagination history-pagination" :aria-label="t('history.pagination')">
      <label>
        {{ t('history.rowsPerPage') }}
        <select :value="limit" :disabled="loading" :aria-label="t('history.rowsPerPage')" @change="changeLimit">
          <option :value="10">10</option>
          <option :value="20">20</option>
          <option :value="50">50</option>
          <option :value="100">100</option>
        </select>
      </label>
      <span>{{ t('history.page', { page }) }}</span>
      <div>
        <button type="button" class="secondary-button" :disabled="page <= 1 || loading" :aria-label="t('history.previousPage')" @click="changePage(page - 1)">{{ t('history.previous') }}</button>
        <button type="button" class="secondary-button" :disabled="!result.has_next || loading" :aria-label="t('history.nextPage')" @click="changePage(page + 1)">{{ t('history.next') }}</button>
      </div>
    </footer>
  </section>
</template>
