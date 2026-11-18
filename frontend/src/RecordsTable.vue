<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
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
import type { ResourceMetadata } from './schema'
import RecordForm from './RecordForm.vue'
import { canCreateRecord, canUpdateRecord, type FormMode } from './recordForm'
import type { RecordRow } from './records'

const props = defineProps<{
  backendBaseURL: string
  schemaName: string
  resource: ResourceMetadata
  writeEnabled: boolean
}>()
const emit = defineEmits<{ 'dirty-change': [dirty: boolean] }>()

const state = ref<RecordsQueryState>(defaultRecordsQuery())
const result = ref<RecordsResponse | null>(null)
const loading = ref(false)
const errorMessage = ref('')
const errorCode = ref('')
const searchInput = ref('')
const searchColumn = ref('')
const filterColumn = ref('')
const filterOperator = ref<FilterOperator>('is_null')
const filterValue = ref('')
const filterBoolean = ref('true')
const filterNull = ref('true')
const formMode = ref<FormMode | null>(null)
const activeRecord = ref<RecordRow | undefined>()

let controller: AbortController | null = null
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
const canCreate = computed(() => canCreateRecord(props.writeEnabled, props.resource))
const canUpdate = computed(() => canUpdateRecord(props.writeEnabled, props.resource))

function openCreateForm() {
  activeRecord.value = undefined
  formMode.value = 'create'
}

function openEditForm(record: RecordRow) {
  activeRecord.value = record
  formMode.value = 'edit'
}

function closeForm() {
  formMode.value = null
  activeRecord.value = undefined
}

function recordSaved() {
  closeForm()
  void loadRecords()
}

function syncFromURL() {
  closeForm()
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
  controller = new AbortController()
  window.clearTimeout(timeout)
  timeout = window.setTimeout(() => controller?.abort(), 12000)
  loading.value = true
  errorMessage.value = ''
  errorCode.value = ''
  result.value = null

  try {
    const response = await fetchRecords(
      props.backendBaseURL,
      props.schemaName,
      props.resource.name,
      state.value,
      controller.signal,
    )
    if (currentRequest === requestID) result.value = response
  } catch (error) {
    if (currentRequest !== requestID) return
    if (error instanceof RecordsRequestError) {
      errorCode.value = error.code
      errorMessage.value = error.message
    } else if (error instanceof DOMException && error.name === 'AbortError') {
      errorMessage.value = 'The records request was cancelled or timed out.'
    } else {
      errorMessage.value = error instanceof Error ? error.message : 'Could not load records'
    }
  } finally {
    window.clearTimeout(timeout)
    if (currentRequest === requestID) loading.value = false
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

function filterLabel(filter: RecordFilter) {
  const labels: Record<FilterOperator, string> = {
    eq: '=',
    neq: '≠',
    contains: 'contains',
    lt: '<',
    lte: '≤',
    gt: '>',
    gte: '≥',
    is_null: filter.value ? 'is null' : 'is not null',
  }
  const value = filter.operator === 'is_null' ? '' : ` “${String(filter.value)}”`
  return `${filter.column} ${labels[filter.operator]}${value}`
}

function changePage(page: number) {
  commit({ ...state.value, page })
}

watch(() => [props.schemaName, props.resource.name], syncFromURL, { immediate: true })
onMounted(() => window.addEventListener('popstate', syncFromURL))
onBeforeUnmount(() => {
  window.removeEventListener('popstate', syncFromURL)
  controller?.abort()
  window.clearTimeout(timeout)
})
</script>

<template>
  <section class="records-section" aria-label="Resource records" :aria-busy="loading">
    <div class="record-controls">
      <button v-if="canCreate" type="button" class="primary-button" @click="openCreateForm">Create record</button>
      <p v-if="!writeEnabled" class="read-only-notice">
        This target is read-only. Set TARGET_WRITE_DATABASE_URL on the backend to enable creating and editing records.
      </p>
      <p v-else-if="resource.kind !== 'table'" class="read-only-notice">Views are read-only.</p>
      <p v-else-if="resource.primary_key.length === 0" class="read-only-notice">
        This table has no primary key. Creating records is available, but editing is disabled.
      </p>
    </div>

    <RecordForm
      v-if="formMode"
      :backend-base-u-r-l="backendBaseURL"
      :schema-name="schemaName"
      :resource="resource"
      :mode="formMode"
      :record="activeRecord"
      @saved="recordSaved"
      @cancel="closeForm"
      @dirty-change="emit('dirty-change', $event)"
    />

    <form class="records-toolbar" @submit.prevent="applySearch">
      <label class="search-field">
        <span>Search</span>
        <input v-model="searchInput" type="search" placeholder="Literal text" />
      </label>
      <label>
        <span>Search column</span>
        <select v-model="searchColumn" :disabled="searchableColumns.length === 0">
          <option value="">All text columns</option>
          <option v-for="column in searchableColumns" :key="column.name" :value="column.name">
            {{ column.name }}
          </option>
        </select>
      </label>
      <button type="submit" class="primary-button">Search</button>
      <button v-if="state.search" type="button" class="secondary-button" @click="clearSearch">Clear</button>
    </form>

    <details class="filters-panel" :open="state.filters.length > 0">
      <summary>Filters <span v-if="state.filters.length">({{ state.filters.length }})</span></summary>
      <form class="filter-form" @submit.prevent="addFilter">
        <label>
          <span>Column</span>
          <select v-model="filterColumn" @change="resetFilterOperator">
            <option v-for="column in resource.columns" :key="column.name" :value="column.name">
              {{ column.name }} · {{ column.data_type }}
            </option>
          </select>
        </label>
        <label>
          <span>Operator</span>
          <select v-model="filterOperator">
            <option v-for="operator in selectedFilterOperators" :key="operator" :value="operator">
              {{ operator }}
            </option>
          </select>
        </label>
        <label v-if="filterOperator === 'is_null'">
          <span>Value</span>
          <select v-model="filterNull">
            <option value="true">null</option>
            <option value="false">not null</option>
          </select>
        </label>
        <label v-else-if="selectedFilterColumn?.data_type === 'boolean'">
          <span>Value</span>
          <select v-model="filterBoolean">
            <option value="true">true</option>
            <option value="false">false</option>
          </select>
        </label>
        <label v-else class="filter-value">
          <span>Value</span>
          <input v-model="filterValue" type="text" autocomplete="off" />
        </label>
        <button type="submit" class="secondary-button" :disabled="!canAddFilter">Add filter</button>
      </form>
      <div v-if="state.filters.length" class="filter-list" aria-label="Active filters">
        <span v-for="(filter, index) in state.filters" :key="`${index}-${filter.column}`" class="filter-chip">
          {{ filterLabel(filter) }}
          <button type="button" :aria-label="`Remove filter ${index + 1}`" @click="removeFilter(index)">×</button>
        </span>
      </div>
    </details>

    <div v-if="loading" class="records-state" role="status">Loading records…</div>
    <div v-else-if="errorMessage" class="records-state records-error" role="alert">
      <strong>Could not load records</strong>
      <p>{{ errorMessage }}</p>
      <code v-if="errorCode">{{ errorCode }}</code>
      <button type="button" class="secondary-button" @click="loadRecords">Try again</button>
    </div>
    <div v-else-if="result?.records.length === 0" class="records-state">
      <strong>No records found</strong>
      <p>Try another page, search term or filter.</p>
    </div>
    <div v-else-if="result" class="table-wrapper records-table-wrapper">
      <table class="records-table">
        <thead>
          <tr>
            <th v-for="column in resource.columns" :key="column.name" :aria-sort="state.sort === column.name ? (state.direction === 'asc' ? 'ascending' : 'descending') : 'none'">
              <button v-if="isSortableColumn(column)" type="button" class="sort-button" @click="sortBy(column.name)">
                {{ column.name }}
                <span v-if="state.sort === column.name" aria-hidden="true">{{ state.direction === 'asc' ? '↑' : '↓' }}</span>
              </button>
              <span v-else>{{ column.name }}</span>
              <small>{{ column.data_type }}</small>
            </th>
            <th v-if="canUpdate">Actions</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(record, rowIndex) in result.records" :key="rowIndex">
            <td
              v-for="column in resource.columns"
              :key="column.name"
              :class="{ 'null-value': record[column.name] === null, 'json-value': typeof record[column.name] === 'object' && record[column.name] !== null }"
            >{{ formatRecordValue(record[column.name], column.data_type) }}</td>
            <td v-if="canUpdate" class="record-action-cell">
              <button type="button" class="secondary-button" @click="openEditForm(record)">Edit</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <footer class="pagination" aria-label="Records pagination">
      <label>
        Rows per page
        <select :value="state.limit" @change="commit({ ...state, page: 1, limit: Number(($event.target as HTMLSelectElement).value) })">
          <option :value="10">10</option>
          <option :value="25">25</option>
          <option :value="50">50</option>
          <option :value="100">100</option>
        </select>
      </label>
      <span>Page {{ state.page }}</span>
      <div>
        <button type="button" class="secondary-button" :disabled="state.page <= 1 || loading" @click="changePage(state.page - 1)">Previous</button>
        <button type="button" class="secondary-button" :disabled="!result?.has_next || loading" @click="changePage(state.page + 1)">Next</button>
      </div>
    </footer>
  </section>
</template>
