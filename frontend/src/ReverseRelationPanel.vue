<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { errorTranslationKey } from './i18n'
import {
  defaultRecordsQuery,
  fetchRecords,
  formatRecordValue,
  RecordsRequestError,
  type RecordRow,
  type RecordsResponse,
} from './records'
import { relationKeyLabel } from './relation'
import { reverseRelationParentFilters } from './relatedRecords'
import type { ColumnMetadata, ResourceMetadata, ReverseRelationMetadata } from './schema'

const props = defineProps<{
  backendBaseURL: string
  relation: ReverseRelationMetadata
  childResource: ResourceMetadata
  parentRecord?: RecordRow
}>()
const { t, locale } = useI18n()
const page = ref(1)
const limit = ref(5)
const result = ref<RecordsResponse | null>(null)
const loading = ref(false)
const errorMessage = ref('')
const errorCode = ref('')
const errorStatus = ref(0)
let controller: AbortController | null = null
let requestID = 0
let timeout = 0

const parentFilters = computed(() => reverseRelationParentFilters(props.relation, props.parentRecord))
const parentFiltersKey = computed(() => JSON.stringify(parentFilters.value))
const visibleColumns = computed(() => {
  const columns: ColumnMetadata[] = []
  const seen = new Set<string>()
  const add = (name: string) => {
    const column = props.childResource.columns.find((item) => item.name === name)
    if (column && !seen.has(name) && columns.length < 6) {
      columns.push(column)
      seen.add(name)
    }
  }
  props.childResource.primary_key.forEach(add)
  for (const name of ['display_name', 'name', 'title', 'label']) add(name)
  for (const column of props.childResource.columns) add(column.name)
  return columns
})

async function loadChildren() {
  const currentRequest = ++requestID
  controller?.abort()
  window.clearTimeout(timeout)
  errorMessage.value = ''
  errorCode.value = ''
  errorStatus.value = 0
  if (!parentFilters.value) {
    loading.value = false
    result.value = null
    return
  }
  controller = new AbortController()
  timeout = window.setTimeout(() => controller?.abort(), 12000)
  loading.value = true
  try {
    const response = await fetchRecords(
      props.backendBaseURL,
      props.relation.schema,
      props.relation.resource,
      {
        ...defaultRecordsQuery(),
        page: page.value,
        limit: limit.value,
        filters: parentFilters.value,
      },
      controller.signal,
    )
    if (currentRequest === requestID) result.value = response
  } catch (error) {
    if (currentRequest !== requestID) return
    result.value = null
    if (error instanceof RecordsRequestError) {
      errorCode.value = error.code
      errorStatus.value = error.status
      errorMessage.value = errorTranslationKey(error.code, error.status)
    } else if (error instanceof DOMException && error.name === 'AbortError') {
      errorMessage.value = 'relation.requestCancelled'
    } else {
      errorMessage.value = 'errors.network'
    }
  } finally {
    window.clearTimeout(timeout)
    if (currentRequest === requestID) loading.value = false
  }
}

function childKey(record: RecordRow): string {
  return props.childResource.primary_key.length
    ? relationKeyLabel(record, props.childResource.primary_key)
    : t('relation.relatedRecord')
}

watch([parentFiltersKey, page, limit, () => props.relation.name], () => void loadChildren(), { immediate: true })
onBeforeUnmount(() => {
  controller?.abort()
  window.clearTimeout(timeout)
})
</script>

<template>
  <section class="reverse-relation-panel" :aria-label="t('relation.childPanelLabel', { resource: childResource.name })" :aria-busy="loading">
    <header class="reverse-relation-heading">
      <div>
        <h4>{{ childResource.name }}</h4>
        <code>{{ relation.columns.join(' + ') }}</code>
      </div>
      <span class="kind-badge">{{ t('relation.readOnly') }}</span>
    </header>

    <div v-if="loading" class="reverse-relation-state" role="status">{{ t('relation.loadingChildren') }}</div>
    <div v-else-if="errorMessage" class="reverse-relation-state reverse-relation-error" role="alert">
      <strong>{{ errorStatus === 401 ? t('errors.unauthenticated') : errorStatus === 403 ? t('errors.forbidden') : t('relation.childrenLoadError') }}</strong>
      <p>{{ t(errorMessage) }}</p>
      <code v-if="errorCode">{{ errorCode }}</code>
      <button type="button" class="secondary-button" @click="loadChildren">{{ t('common.retry') }}</button>
    </div>
    <p v-else-if="!parentFilters" class="reverse-relation-state">{{ t('relation.emptyRelation') }}</p>
    <p v-else-if="result?.records.length === 0" class="reverse-relation-state">{{ t('relation.noChildRecords') }}</p>
    <ul v-else-if="result" class="reverse-relation-records">
      <li v-for="record in result.records" :key="childKey(record)" :aria-label="t('relation.childRecordLabel', { key: childKey(record) })">
        <code class="reverse-relation-key">{{ childKey(record) }}</code>
        <dl>
          <template v-for="column in visibleColumns" :key="column.name">
            <div>
              <dt>{{ column.name }}</dt>
              <dd :title="formatRecordValue(record[column.name], column.data_type, locale)">
                {{ formatRecordValue(record[column.name], column.data_type, locale) }}
              </dd>
            </div>
          </template>
        </dl>
      </li>
    </ul>

    <footer v-if="parentFilters" class="reverse-relation-pagination" :aria-label="t('relation.childPagination', { resource: childResource.name })">
      <label>
        {{ t('relation.rowsPerPage') }}
        <select v-model.number="limit" :disabled="loading" @change="page = 1">
          <option :value="5">5</option>
          <option :value="10">10</option>
          <option :value="20">20</option>
        </select>
      </label>
      <span>{{ t('relation.page', { page }) }}</span>
      <div>
        <button type="button" class="secondary-button" :disabled="page <= 1 || loading" :aria-label="t('relation.previousChildPage', { resource: childResource.name })" @click="page--">{{ t('relation.previous') }}</button>
        <button type="button" class="secondary-button" :disabled="!result?.has_next || loading" :aria-label="t('relation.nextChildPage', { resource: childResource.name })" @click="page++">{{ t('relation.next') }}</button>
      </div>
    </footer>
  </section>
</template>
