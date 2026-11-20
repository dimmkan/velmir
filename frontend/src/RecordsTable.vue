<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { errorTranslationKey } from './i18n'
import {
  buildRecordsQuery,
  defaultRecordsQuery,
  fetchRecords,
  filterOperators,
  formatRecordValue,
  isSortableColumn,
  isTextColumn,
  parseRecordsQuery,
  RecordsRequestError,
  type FilterOperator,
  type RecordFilter,
  type RecordsQueryState,
  type RecordsResponse,
} from './records'
import type { ResourceMetadata, SchemaMetadata } from './schema'
import RecordForm from './RecordForm.vue'
import RecordHistory from './RecordHistory.vue'
import RecordDeleteDialog from './RecordDeleteDialog.vue'
import { canCreateRecord, canUpdateRecord, type FormMode } from './recordForm'
import { buildRecordKey, deleteRecord, MutationRequestError } from './mutations'
import type { RecordRow } from './records'
import {
  buildRelatedRelationPlans,
  fetchRelatedLabels,
  localRelationKey,
} from './relatedRecords'
import type { ForeignKeyMetadata } from './schema'
import type { RelationOption } from './relation'

const props = defineProps<{
  backendBaseURL: string
  schemaName: string
  resource: ResourceMetadata
  schemas?: SchemaMetadata[]
  writeEnabled: boolean
  readAllowed: boolean
  createAllowed: boolean
  updateAllowed: boolean
  deleteAllowed: boolean
}>()
const emit = defineEmits<{ 'dirty-change': [dirty: boolean] }>()
const { t, locale } = useI18n()

const state = ref<RecordsQueryState>(defaultRecordsQuery())
const result = ref<RecordsResponse | null>(null)
const loading = ref(false)
const errorMessage = ref('')
const errorCode = ref('')
const errorStatus = ref(0)
const searchInput = ref('')
const searchColumn = ref('')
const filterColumn = ref('')
const filterOperator = ref<FilterOperator>('is_null')
const filterValue = ref('')
const filterBoolean = ref('true')
const filterNull = ref('true')
const formMode = ref<FormMode | null>(null)
const activeRecord = ref<RecordRow | undefined>()
const historyKey = ref('')
const auditWarning = ref<'save' | 'delete' | ''>('')
const deleteTarget = ref<RecordRow | null>(null)
const deleting = ref(false)
const deleteErrorMessage = ref('')
const deleteSuccess = ref(false)
const relatedLabels = ref<Map<string, Map<string, RelationOption>>>(new Map())
const relatedLabelsLoading = ref(false)
const relatedLabelsFailed = ref(false)

let controller: AbortController | null = null
let relatedController: AbortController | null = null
let requestID = 0
let timeout = 0

const searchableColumns = computed(() => props.resource.columns.filter(isTextColumn))
const selectedFilterColumn = computed(() =>
  props.resource.columns.find((column) => column.name === filterColumn.value),
)
const selectedFilterOperators = computed(() =>
  selectedFilterColumn.value ? filterOperators(selectedFilterColumn.value.data_type) : (['is_null'] as FilterOperator[]),
)
const canAddFilter = computed(() =>
  Boolean(filterColumn.value) && (filterOperator.value === 'is_null' || filterValue.value !== ''),
)
const canCreate = computed(() => canCreateRecord(props.writeEnabled, props.resource, props.createAllowed))
const canUpdate = computed(() => canUpdateRecord(props.writeEnabled, props.resource, props.updateAllowed))
const canDelete = computed(() => props.writeEnabled && props.deleteAllowed && props.resource.kind === 'table' && props.resource.primary_key.length > 0)
const canViewHistory = computed(() => props.readAllowed && props.resource.primary_key.length > 0)
const canViewDetails = computed(() => props.readAllowed)
const hasMutationPermission = computed(() => props.createAllowed || props.updateAllowed || props.deleteAllowed)

function relationForColumn(columnName: string): ForeignKeyMetadata | null {
  return props.resource.foreign_keys.find((relation) => relation.columns[0] === columnName) ?? null
}

function relationIsNull(relation: ForeignKeyMetadata, record: RecordRow): boolean {
  return relation.columns.every((column) => record[column] === null || record[column] === undefined)
}

function relatedOption(relation: ForeignKeyMetadata, record: RecordRow): RelationOption | undefined {
  return relatedLabels.value.get(relation.name)?.get(localRelationKey(relation, record))
}

function relatedCellTitle(relation: ForeignKeyMetadata, record: RecordRow): string {
  if (relationIsNull(relation, record)) return t('relation.emptyRelation')
  const option = relatedOption(relation, record)
  if (option) return t('relation.relatedRecordTitle', { label: option.label, key: option.keyLabel })
  if (relatedLabelsLoading.value) return t('relation.loading')
  if (relatedLabelsFailed.value) return t('relation.rawFallback')
  return t('relation.unavailableLabel')
}

function openCreateForm() {
  historyKey.value = ''
  activeRecord.value = undefined
  formMode.value = 'create'
}

function openEditForm(record: RecordRow) {
  historyKey.value = ''
  activeRecord.value = record
  formMode.value = 'edit'
}

function openHistory(record: RecordRow) {
  closeForm()
  auditWarning.value = ''
  historyKey.value = buildRecordKey(props.resource, record)
}

function closeForm() {
  formMode.value = null
  activeRecord.value = undefined
}

function recordSaved() {
  auditWarning.value = ''
  closeForm()
  void loadRecords()
}

function recordAuditFailed() {
  closeForm()
  auditWarning.value = 'save'
  void loadRecords()
}

const deleteTargetKey = computed(() =>
  deleteTarget.value ? buildRecordKey(props.resource, deleteTarget.value) : '',
)

function openDeleteDialog(record: RecordRow, event: Event) {
  if (event.currentTarget instanceof HTMLElement) event.currentTarget.focus()
  closeForm()
  historyKey.value = ''
  auditWarning.value = ''
  deleteSuccess.value = false
  deleteErrorMessage.value = ''
  deleteTarget.value = record
}

function closeDeleteDialog() {
  if (deleting.value) return
  deleteTarget.value = null
  deleteErrorMessage.value = ''
}

async function confirmDelete() {
  if (deleting.value || !deleteTarget.value) return
  const key = deleteTargetKey.value
  const schemaName = props.schemaName
  const resourceName = props.resource.name
  const wasLastRecordOnPage = result.value?.records.length === 1
  deleting.value = true
  deleteErrorMessage.value = ''
  try {
    await deleteRecord(props.backendBaseURL, schemaName, resourceName, key)
    if (result.value) {
      result.value = {
        ...result.value,
        records: result.value.records.filter((record) => buildRecordKey(props.resource, record) !== key),
      }
    }
    deleteTarget.value = null
    if (wasLastRecordOnPage && state.value.page > 1) {
      commit({ ...state.value, page: state.value.page - 1 })
    } else {
      await loadRecords()
    }
    deleteSuccess.value = true
  } catch (error) {
    if (error instanceof MutationRequestError && error.code === 'AUDIT_WRITE_FAILED' && error.mutationCommitted) {
      deleteTarget.value = null
      auditWarning.value = 'delete'
      await loadRecords()
    } else if (error instanceof MutationRequestError) {
      deleteErrorMessage.value = ['FOREIGN_KEY_VIOLATION', 'CHECK_VIOLATION', 'NOT_NULL_VIOLATION', 'UNIQUE_VIOLATION'].includes(error.code)
        ? 'records.delete.constraint'
        : errorTranslationKey(error.code, error.status)
    } else {
      deleteErrorMessage.value = 'errors.network'
    }
  } finally {
    deleting.value = false
  }
}

function syncFromURL() {
  closeForm()
  closeDeleteDialog()
  historyKey.value = ''
  auditWarning.value = ''
  deleteSuccess.value = false
  state.value = parseRecordsQuery(window.location.search, props.resource)
  searchInput.value = state.value.search
  searchColumn.value = state.value.searchColumns.length === 1 ? state.value.searchColumns[0] : ''
  if (!props.resource.columns.some((column) => column.name === filterColumn.value)) {
    filterColumn.value = props.resource.columns[0]?.name ?? ''
  }
  resetFilterOperator()
  void loadRecords()
}

function updateURL(next: RecordsQueryState, push = true) {
  const url = new URL(window.location.href)
  for (const key of ['page', 'limit', 'sort', 'direction', 'search', 'search_column', 'filter']) {
    url.searchParams.delete(key)
  }
  const query = buildRecordsQuery(next)
  for (const [key, value] of query) url.searchParams.append(key, value)
  window.history[push ? 'pushState' : 'replaceState']({}, '', `${url.pathname}${url.search}${url.hash}`)
}

function commit(next: RecordsQueryState) {
  state.value = next
  updateURL(next)
  void loadRecords()
}

async function loadRecords() {
  const currentRequest = ++requestID
  controller?.abort()
  relatedController?.abort()
  controller = new AbortController()
  window.clearTimeout(timeout)
  timeout = window.setTimeout(() => controller?.abort(), 12000)
  loading.value = true
  errorMessage.value = ''
  errorCode.value = ''
  errorStatus.value = 0
  result.value = null
  relatedLabels.value = new Map()
  relatedLabelsLoading.value = false
  relatedLabelsFailed.value = false

  try {
    const response = await fetchRecords(
      props.backendBaseURL,
      props.schemaName,
      props.resource.name,
      state.value,
      controller.signal,
    )
    if (currentRequest === requestID) {
      result.value = response
      void loadRelatedLabels(response.records, currentRequest)
    }
  } catch (error) {
    if (currentRequest !== requestID) return
    if (error instanceof RecordsRequestError) {
      errorCode.value = error.code
      errorStatus.value = error.status
      errorMessage.value = errorTranslationKey(error.code, error.status)
    } else if (error instanceof DOMException && error.name === 'AbortError') {
      errorMessage.value = 'records.requestCancelled'
    } else {
      errorMessage.value = 'errors.generic'
    }
  } finally {
    window.clearTimeout(timeout)
    if (currentRequest === requestID) loading.value = false
  }
}

async function loadRelatedLabels(records: RecordRow[], currentRequest: number) {
  const plans = buildRelatedRelationPlans(props.resource, records, props.schemas ?? [])
  if (plans.length === 0 || plans.every((plan) => plan.keys.length === 0)) return
  relatedController = new AbortController()
  const relatedTimeout = window.setTimeout(() => relatedController?.abort(), 12000)
  relatedLabelsLoading.value = true
  try {
    const labels = await fetchRelatedLabels(
      props.backendBaseURL,
      props.schemaName,
      props.resource.name,
      plans,
      relatedController.signal,
    )
    if (currentRequest === requestID) relatedLabels.value = labels
  } catch (error) {
    if (currentRequest !== requestID || error instanceof DOMException && error.name === 'AbortError') return
    relatedLabelsFailed.value = true
  } finally {
    window.clearTimeout(relatedTimeout)
    if (currentRequest === requestID) relatedLabelsLoading.value = false
  }
}

function sortBy(columnName: string) {
  const direction = state.value.sort === columnName && state.value.direction === 'asc' ? 'desc' : 'asc'
  commit({ ...state.value, page: 1, sort: columnName, direction })
}

function applySearch() {
  commit({
    ...state.value,
    page: 1,
    search: searchInput.value,
    searchColumns: searchColumn.value ? [searchColumn.value] : [],
  })
}

function clearSearch() {
  searchInput.value = ''
  searchColumn.value = ''
  commit({ ...state.value, page: 1, search: '', searchColumns: [] })
}

function resetFilterOperator() {
  const operators = selectedFilterOperators.value
  if (!operators.includes(filterOperator.value)) filterOperator.value = operators[0]
}

function addFilter() {
  if (!canAddFilter.value) return
  let value: string | boolean = filterValue.value
  if (filterOperator.value === 'is_null') value = filterNull.value === 'true'
  else if (selectedFilterColumn.value?.data_type === 'boolean') value = filterBoolean.value
  const filter: RecordFilter = { column: filterColumn.value, operator: filterOperator.value, value }
  commit({ ...state.value, page: 1, filters: [...state.value.filters, filter] })
  filterValue.value = ''
}

function removeFilter(index: number) {
  commit({ ...state.value, page: 1, filters: state.value.filters.filter((_, itemIndex) => itemIndex !== index) })
}

function operatorLabel(operator: FilterOperator, value?: string | boolean) {
  if (operator === 'is_null') return t(value ? 'records.operators.isNull' : 'records.operators.isNotNull')
  return t(`records.operators.${operator}`)
}

function filterLabel(filter: RecordFilter) {
  const value = filter.operator === 'is_null' ? '' : ` “${String(filter.value)}”`
  return `${filter.column} ${operatorLabel(filter.operator, filter.value)}${value}`
}

function changePage(page: number) {
  commit({ ...state.value, page })
}

watch(() => [props.schemaName, props.resource.name], syncFromURL, { immediate: true })
onMounted(() => window.addEventListener('popstate', syncFromURL))
onBeforeUnmount(() => {
  window.removeEventListener('popstate', syncFromURL)
  controller?.abort()
  relatedController?.abort()
  window.clearTimeout(timeout)
})
</script>

<template>
  <section class="records-section" :aria-label="t('records.sectionLabel')" :aria-busy="loading">
    <div class="record-controls">
      <button v-if="canCreate" type="button" class="primary-button" @click="openCreateForm">{{ t('records.create') }}</button>
      <p v-if="!hasMutationPermission" class="read-only-notice">{{ t('records.readOnlyPermission') }}</p>
      <p v-else-if="!writeEnabled" class="read-only-notice">{{ t('records.readOnlyTarget') }}</p>
      <p v-else-if="resource.kind !== 'table'" class="read-only-notice">{{ t('records.readOnlyView') }}</p>
      <p v-else-if="resource.primary_key.length === 0" class="read-only-notice">{{ t('records.noPrimaryKey') }}</p>
    </div>

    <RecordForm
      v-if="formMode"
      :backend-base-u-r-l="backendBaseURL"
      :schema-name="schemaName"
      :resource="resource"
      :schemas="schemas ?? []"
      :mode="formMode"
      :record="activeRecord"
      :update-allowed="canUpdate"
      @saved="recordSaved"
      @audit-failed="recordAuditFailed"
      @cancel="closeForm"
      @dirty-change="emit('dirty-change', $event)"
    />

    <RecordDeleteDialog
      v-if="deleteTarget"
      :resource-name="`${schemaName}.${resource.name}`"
      :record-key="deleteTargetKey"
      :pending="deleting"
      :error-message="deleteErrorMessage"
      @cancel="closeDeleteDialog"
      @confirm="confirmDelete"
    />

    <div v-if="deleteSuccess" class="delete-success" role="status">{{ t('records.delete.success') }}</div>

    <div v-if="auditWarning" class="audit-warning" role="alert">
      <strong>{{ t(auditWarning === 'delete' ? 'history.deleteAuditFailedTitle' : 'history.auditFailedTitle') }}</strong>
      <p>{{ t(auditWarning === 'delete' ? 'history.deleteAuditFailed' : 'history.auditFailed') }}</p>
      <button type="button" class="secondary-button" :disabled="loading" @click="loadRecords">{{ t('history.refreshRecords') }}</button>
    </div>

    <RecordHistory
      v-if="historyKey"
      :backend-base-u-r-l="backendBaseURL"
      :schema-name="schemaName"
      :resource="resource"
      :record-key="historyKey"
      @close="historyKey = ''"
    />

    <form class="records-toolbar" @submit.prevent="applySearch">
      <label class="search-field">
        <span>{{ t('records.search') }}</span>
        <input v-model="searchInput" type="search" :placeholder="t('records.searchPlaceholder')" />
      </label>
      <label>
        <span>{{ t('records.searchColumn') }}</span>
        <select v-model="searchColumn" :disabled="searchableColumns.length === 0">
          <option value="">{{ t('records.allTextColumns') }}</option>
          <option v-for="column in searchableColumns" :key="column.name" :value="column.name">{{ column.name }}</option>
        </select>
      </label>
      <button type="submit" class="primary-button">{{ t('records.search') }}</button>
      <button v-if="state.search" type="button" class="secondary-button" @click="clearSearch">{{ t('common.clear') }}</button>
    </form>

    <details class="filters-panel" :open="state.filters.length > 0">
      <summary>{{ t('records.filters') }} <span v-if="state.filters.length">({{ state.filters.length }})</span></summary>
      <form class="filter-form" @submit.prevent="addFilter">
        <label>
          <span>{{ t('records.column') }}</span>
          <select v-model="filterColumn" @change="resetFilterOperator">
            <option v-for="column in resource.columns" :key="column.name" :value="column.name">{{ column.name }} · {{ column.data_type }}</option>
          </select>
        </label>
        <label>
          <span>{{ t('records.operator') }}</span>
          <select v-model="filterOperator">
            <option v-for="operator in selectedFilterOperators" :key="operator" :value="operator">{{ operatorLabel(operator) }}</option>
          </select>
        </label>
        <label v-if="filterOperator === 'is_null'">
          <span>{{ t('records.value') }}</span>
          <select v-model="filterNull">
            <option value="true">{{ t('records.null') }}</option>
            <option value="false">{{ t('records.notNull') }}</option>
          </select>
        </label>
        <label v-else-if="selectedFilterColumn?.data_type === 'boolean'">
          <span>{{ t('records.value') }}</span>
          <select v-model="filterBoolean">
            <option value="true">true</option>
            <option value="false">false</option>
          </select>
        </label>
        <label v-else class="filter-value">
          <span>{{ t('records.value') }}</span>
          <input v-model="filterValue" type="text" autocomplete="off" />
        </label>
        <button type="submit" class="secondary-button" :disabled="!canAddFilter">{{ t('records.addFilter') }}</button>
      </form>
      <div v-if="state.filters.length" class="filter-list" :aria-label="t('records.activeFilters')">
        <span v-for="(filter, index) in state.filters" :key="`${index}-${filter.column}`" class="filter-chip">
          {{ filterLabel(filter) }}
          <button type="button" :aria-label="t('records.removeFilter', { number: index + 1 })" @click="removeFilter(index)">×</button>
        </span>
      </div>
    </details>

    <div v-if="loading" class="records-state" role="status">{{ t('records.loading') }}</div>
    <div v-else-if="errorMessage" class="records-state records-error" role="alert">
      <strong>{{ errorStatus === 403 ? t('errors.forbidden') : t('records.loadFailed') }}</strong>
      <p>{{ t(errorMessage) }}</p>
      <code v-if="errorCode">{{ errorCode }}</code>
      <button type="button" class="secondary-button" @click="loadRecords">{{ t('common.retry') }}</button>
    </div>
    <div v-else-if="result?.records.length === 0" class="records-state">
      <strong>{{ t('records.empty') }}</strong>
      <p>{{ t('records.emptyHint') }}</p>
    </div>
    <div v-else-if="result" class="table-wrapper records-table-wrapper">
      <table class="records-table">
        <thead>
          <tr>
            <th v-for="column in resource.columns" :key="column.name" :aria-sort="state.sort === column.name ? (state.direction === 'asc' ? 'ascending' : 'descending') : 'none'">
              <button v-if="isSortableColumn(column)" type="button" class="sort-button" :aria-label="t('records.sortBy', { column: column.name })" @click="sortBy(column.name)">
                {{ column.name }}
                <span v-if="state.sort === column.name" aria-hidden="true">{{ state.direction === 'asc' ? '↑' : '↓' }}</span>
              </button>
              <span v-else>{{ column.name }}</span>
              <small>{{ column.data_type }}</small>
            </th>
            <th v-if="canUpdate || canViewDetails || canViewHistory || canDelete">{{ t('records.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(record, rowIndex) in result.records" :key="rowIndex">
            <td
              v-for="column in resource.columns"
              :key="column.name"
              :class="{ 'null-value': record[column.name] === null, 'json-value': typeof record[column.name] === 'object' && record[column.name] !== null }"
            >
              <div
                v-if="relationForColumn(column.name)"
                class="related-record-cell"
                :title="relatedCellTitle(relationForColumn(column.name)!, record)"
                :aria-label="relatedCellTitle(relationForColumn(column.name)!, record)"
              >
                <span v-if="relationIsNull(relationForColumn(column.name)!, record)" class="null-relation">
                  {{ t('relation.emptyRelation') }}
                </span>
                <template v-else-if="relatedOption(relationForColumn(column.name)!, record)">
                  <span class="related-record-label">{{ relatedOption(relationForColumn(column.name)!, record)!.label }}</span>
                  <code v-if="relatedOption(relationForColumn(column.name)!, record)!.label !== relatedOption(relationForColumn(column.name)!, record)!.keyLabel">
                    {{ relatedOption(relationForColumn(column.name)!, record)!.keyLabel }}
                  </code>
                </template>
                <span v-else class="related-record-raw">{{ formatRecordValue(record[column.name], column.data_type, locale) }}</span>
              </div>
              <template v-else>{{ formatRecordValue(record[column.name], column.data_type, locale) }}</template>
            </td>
            <td v-if="canUpdate || canViewDetails || canViewHistory || canDelete" class="record-action-cell">
              <div class="record-row-actions">
                <button v-if="canUpdate || canViewDetails" type="button" class="secondary-button record-details-button" @click="openEditForm(record)">{{ t(canUpdate ? 'common.edit' : 'common.view') }}</button>
                <button v-if="canViewHistory" type="button" class="secondary-button" @click="openHistory(record)">{{ t('history.open') }}</button>
                <button
                  v-if="canDelete"
                  type="button"
                  class="danger-button record-delete-button"
                  :aria-label="t('records.delete.recordLabel', { key: buildRecordKey(resource, record) })"
                  @click="openDeleteDialog(record, $event)"
                >{{ t('records.delete.action') }}</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <footer class="pagination" :aria-label="t('records.pagination')">
      <label>
        {{ t('records.rowsPerPage') }}
        <select :value="state.limit" @change="commit({ ...state, page: 1, limit: Number(($event.target as HTMLSelectElement).value) })">
          <option :value="10">10</option>
          <option :value="25">25</option>
          <option :value="50">50</option>
          <option :value="100">100</option>
        </select>
      </label>
      <span>{{ t('records.page', { page: state.page }) }}</span>
      <div>
        <button type="button" class="secondary-button" :disabled="state.page <= 1 || loading" @click="changePage(state.page - 1)">{{ t('records.previous') }}</button>
        <button type="button" class="secondary-button" :disabled="!result?.has_next || loading" @click="changePage(state.page + 1)">{{ t('records.next') }}</button>
      </div>
    </footer>
  </section>
</template>
