<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { errorTranslationKey } from './i18n'
import {
  buildRecordKey,
  createRecord,
  deleteRecord,
  MutationRequestError,
} from './mutations'
import {
  defaultRecordsQuery,
  fetchRecords,
  RecordsRequestError,
  type RecordRow,
  type RecordsResponse,
} from './records'
import { relationKeyLabel, relationOption, type RelationOption } from './relation'
import { buildRelatedRelationPlans, fetchRelatedLabels } from './relatedRecords'
import {
  canAddManyToManyRelation,
  canRemoveManyToManyRelation,
  manyToManyCreateValues,
  manyToManyParentFilters,
  manyToManyTargetKey,
  manyToManyTargetKeyRecord,
  type ManyToManyRelation,
} from './manyToMany'
import type { SchemaMetadata } from './schema'
import RelationSelector from './RelationSelector.vue'
import RelationRemoveDialog from './RelationRemoveDialog.vue'

const props = defineProps<{
  backendBaseURL: string
  parentName: string
  relation: ManyToManyRelation
  schemas: SchemaMetadata[]
  parentRecord?: RecordRow
  writeEnabled: boolean
}>()
const { t } = useI18n()
const page = ref(1)
const limit = ref(5)
const result = ref<RecordsResponse | null>(null)
const labels = ref<Map<string, RelationOption>>(new Map())
const loading = ref(false)
const labelsFailed = ref(false)
const errorMessage = ref('')
const errorCode = ref('')
const errorStatus = ref(0)
const selectedRecord = ref<RecordRow | null>(null)
const adding = ref(false)
const removing = ref(false)
const removeTarget = ref<RecordRow | null>(null)
const mutationError = ref('')
const notice = ref<'added' | 'removed' | ''>('')
const auditWarning = ref<'create' | 'delete' | ''>('')
let controller: AbortController | null = null
let requestID = 0
let timeout = 0

const parentFilters = computed(() => manyToManyParentFilters(props.relation, props.parentRecord))
const parentFiltersKey = computed(() => JSON.stringify(parentFilters.value))
const canAdd = computed(() => canAddManyToManyRelation(props.relation, props.writeEnabled))
const canRemove = computed(() => canRemoveManyToManyRelation(props.relation, props.writeEnabled))
const linkedTargetKeys = computed(() => new Set((result.value?.records ?? []).map((record) => manyToManyTargetKey(props.relation, record))))
const selectedValues = computed(() => selectedRecord.value
  ? props.relation.targetForeignKey.referenced_columns.map((column) => selectedRecord.value?.[column])
  : [])
const selectedComplete = computed(() => selectedValues.value.length > 0 && selectedValues.value.every((value) =>
  value !== null && value !== undefined && typeof value !== 'object',
))
const targetName = computed(() => `${props.relation.targetForeignKey.referenced_schema}.${props.relation.targetResource.name}`)
const junctionName = computed(() => `${props.relation.junctionSchema}.${props.relation.junctionResource.name}`)
const selectorInputID = computed(() => `many-to-many-${props.relation.id}`)
const removeTargetKey = computed(() => removeTarget.value
  ? buildRecordKey(props.relation.junctionResource, removeTarget.value)
  : '')
const removeTargetLabel = computed(() => removeTarget.value ? targetOption(removeTarget.value).label : '')

function targetOption(record: RecordRow): RelationOption {
  const key = manyToManyTargetKey(props.relation, record)
  return labels.value.get(key) ?? relationOption(
    manyToManyTargetKeyRecord(props.relation, record),
    props.relation.targetResource,
    props.relation.targetForeignKey.referenced_columns,
  )
}

async function loadRelations() {
  const currentRequest = ++requestID
  controller?.abort()
  window.clearTimeout(timeout)
  errorMessage.value = ''
  errorCode.value = ''
  errorStatus.value = 0
  labelsFailed.value = false
  if (!parentFilters.value) {
    loading.value = false
    result.value = null
    labels.value = new Map()
    return
  }

  controller = new AbortController()
  timeout = window.setTimeout(() => controller?.abort(), 12000)
  loading.value = true
  result.value = null
  labels.value = new Map()
  try {
    const response = await fetchRecords(
      props.backendBaseURL,
      props.relation.junctionSchema,
      props.relation.junctionResource.name,
      {
        ...defaultRecordsQuery(),
        page: page.value,
        limit: limit.value,
        filters: parentFilters.value,
      },
      controller.signal,
    )
    if (currentRequest !== requestID) return
    result.value = response
    const plans = buildRelatedRelationPlans(
      props.relation.junctionResource,
      response.records,
      props.schemas,
    ).filter((plan) => plan.foreignKey.name === props.relation.targetForeignKey.name)
    if (plans.length > 0 && plans.some((plan) => plan.keys.length > 0)) {
      const resolved = await fetchRelatedLabels(
        props.backendBaseURL,
        props.relation.junctionSchema,
        props.relation.junctionResource.name,
        plans,
        controller.signal,
      )
      if (currentRequest === requestID) labels.value = resolved.get(props.relation.targetForeignKey.name) ?? new Map()
    }
  } catch (error) {
    if (currentRequest !== requestID) return
    if (error instanceof DOMException && error.name === 'AbortError') {
      result.value = null
      errorMessage.value = 'relation.requestCancelled'
    } else if (error instanceof RecordsRequestError) {
      result.value = null
      errorCode.value = error.code
      errorStatus.value = error.status
      errorMessage.value = errorTranslationKey(error.code, error.status)
    } else if (result.value) {
      labelsFailed.value = true
    } else {
      result.value = null
      errorMessage.value = 'errors.network'
    }
  } finally {
    window.clearTimeout(timeout)
    if (currentRequest === requestID) loading.value = false
  }
}

function selectTarget(record: RecordRow) {
  selectedRecord.value = record
  mutationError.value = ''
  notice.value = ''
  auditWarning.value = ''
}

async function addRelation() {
  if (adding.value || !canAdd.value || !selectedRecord.value || !props.parentRecord) return
  const values = manyToManyCreateValues(props.relation, props.parentRecord, selectedRecord.value)
  if (!values) return
  adding.value = true
  mutationError.value = ''
  notice.value = ''
  auditWarning.value = ''
  try {
    await createRecord(
      props.backendBaseURL,
      props.relation.junctionSchema,
      props.relation.junctionResource.name,
      values,
    )
    selectedRecord.value = null
    notice.value = 'added'
    await loadRelations()
  } catch (error) {
    if (error instanceof MutationRequestError && error.code === 'AUDIT_WRITE_FAILED' && error.mutationCommitted) {
      selectedRecord.value = null
      auditWarning.value = 'create'
      await loadRelations()
    } else if (error instanceof MutationRequestError) {
      mutationError.value = error.code === 'UNIQUE_VIOLATION'
        ? 'relation.duplicateRelation'
        : errorTranslationKey(error.code, error.status)
    } else {
      mutationError.value = 'errors.network'
    }
  } finally {
    adding.value = false
  }
}

function openRemoveDialog(record: RecordRow, event: Event) {
  if (!canRemove.value || removing.value) return
  if (event.currentTarget instanceof HTMLElement) event.currentTarget.focus()
  removeTarget.value = record
  mutationError.value = ''
  notice.value = ''
  auditWarning.value = ''
}

function closeRemoveDialog() {
  if (removing.value) return
  removeTarget.value = null
  mutationError.value = ''
}

async function refreshAfterRemoval() {
  const wasLastRecordOnPage = result.value?.records.length === 1
  removeTarget.value = null
  if (wasLastRecordOnPage && page.value > 1) page.value--
  else await loadRelations()
}

async function removeRelation() {
  if (removing.value || !canRemove.value || !removeTarget.value) return
  removing.value = true
  mutationError.value = ''
  try {
    await deleteRecord(
      props.backendBaseURL,
      props.relation.junctionSchema,
      props.relation.junctionResource.name,
      removeTargetKey.value,
    )
    await refreshAfterRemoval()
    notice.value = 'removed'
  } catch (error) {
    if (error instanceof MutationRequestError && error.code === 'AUDIT_WRITE_FAILED' && error.mutationCommitted) {
      await refreshAfterRemoval()
      auditWarning.value = 'delete'
    } else if (error instanceof MutationRequestError) {
      mutationError.value = ['FOREIGN_KEY_VIOLATION', 'CHECK_VIOLATION', 'NOT_NULL_VIOLATION', 'UNIQUE_VIOLATION'].includes(error.code)
        ? 'relation.removeRelationConstraint'
        : errorTranslationKey(error.code, error.status)
    } else {
      mutationError.value = 'errors.network'
    }
  } finally {
    removing.value = false
  }
}

watch([parentFiltersKey, page, limit, () => props.relation.id], () => void loadRelations(), { immediate: true })
onBeforeUnmount(() => {
  controller?.abort()
  window.clearTimeout(timeout)
})
</script>

<template>
  <section
    class="reverse-relation-panel many-to-many-panel"
    :aria-label="t('relation.manyToManyPanelLabel', { resource: targetName })"
    :aria-busy="loading || adding || removing"
  >
    <header class="reverse-relation-heading">
      <div>
        <h4>{{ targetName }}</h4>
        <code>{{ relation.id }}</code>
      </div>
      <span class="kind-badge">{{ t('relation.manyToMany') }}</span>
    </header>

    <p v-if="!parentFilters" class="reverse-relation-state" role="status">{{ t('relation.saveRecordFirst') }}</p>
    <template v-else>
      <div v-if="canAdd" class="many-to-many-add">
        <label :for="selectorInputID">{{ t('relation.searchTarget', { resource: targetName }) }}</label>
        <RelationSelector
          :backend-base-u-r-l="backendBaseURL"
          :schema-name="relation.targetForeignKey.referenced_schema"
          :resource="relation.targetResource"
          :referenced-columns="relation.targetForeignKey.referenced_columns"
          :selected-values="selectedValues"
          :has-selection="Boolean(selectedRecord)"
          :input-id="selectorInputID"
          :label="targetName"
          :placeholder="t('relation.searchTarget', { resource: targetName })"
          :nullable="false"
          :required="true"
          :disabled="adding || removing"
          :disabled-keys="[...linkedTargetKeys]"
          @select="selectTarget"
        />
        <button
          type="button"
          class="primary-button"
          :disabled="!selectedComplete || adding || removing"
          :aria-busy="adding"
          @click="addRelation"
        >{{ adding ? t('relation.addingRelation') : t('relation.addRelation') }}</button>
      </div>

      <div v-if="loading" class="reverse-relation-state" role="status">{{ t('relation.loadingLinked') }}</div>
      <div v-else-if="errorMessage" class="reverse-relation-state reverse-relation-error" role="alert">
        <strong>{{ errorStatus === 401 ? t('errors.unauthenticated') : errorStatus === 403 ? t('errors.forbidden') : t('relation.linkedLoadError') }}</strong>
        <p>{{ t(errorMessage) }}</p>
        <code v-if="errorCode">{{ errorCode }}</code>
        <button type="button" class="secondary-button" @click="loadRelations">{{ t('common.retry') }}</button>
      </div>
      <p v-else-if="result?.records.length === 0" class="reverse-relation-state">{{ t('relation.noLinkedRecords') }}</p>
      <template v-else-if="result">
        <div v-if="labelsFailed" class="many-to-many-label-warning" role="alert">
          <span>{{ t('relation.rawFallback') }}</span>
          <button type="button" class="secondary-button" @click="loadRelations">{{ t('common.retry') }}</button>
        </div>
        <ul class="reverse-relation-records many-to-many-records">
          <li
            v-for="record in result.records"
            :key="buildRecordKey(relation.junctionResource, record)"
            :aria-label="t('relation.linkedRecordLabel', { label: targetOption(record).label, key: targetOption(record).keyLabel })"
          >
            <div class="many-to-many-record">
              <div class="many-to-many-target">
                <strong :title="targetOption(record).label">{{ targetOption(record).label }}</strong>
                <code>{{ targetOption(record).keyLabel }}</code>
              </div>
              <button
                v-if="canRemove"
                type="button"
                class="danger-button many-to-many-remove"
                :aria-label="t('relation.removeRelationLabel', { label: targetOption(record).label, key: targetOption(record).keyLabel })"
                @click="openRemoveDialog(record, $event)"
              >{{ t('relation.removeRelation') }}</button>
            </div>
          </li>
        </ul>
      </template>

      <p v-if="mutationError && !removeTarget" class="form-server-error" role="alert">{{ t(mutationError) }}</p>
      <div v-if="notice" class="delete-success" role="status">
        {{ t(notice === 'added' ? 'relation.relationAdded' : 'relation.relationRemoved') }}
      </div>
      <div v-if="auditWarning" class="audit-warning" role="alert">
        <strong>{{ t(auditWarning === 'create' ? 'relation.createAuditFailedTitle' : 'relation.deleteAuditFailedTitle') }}</strong>
        <p>{{ t(auditWarning === 'create' ? 'relation.createAuditFailed' : 'relation.deleteAuditFailed') }}</p>
      </div>

      <footer class="reverse-relation-pagination" :aria-label="t('relation.manyToManyPagination', { resource: targetName })">
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
          <button type="button" class="secondary-button" :disabled="page <= 1 || loading" :aria-label="t('relation.previousLinkedPage', { resource: targetName })" @click="page--">{{ t('relation.previous') }}</button>
          <button type="button" class="secondary-button" :disabled="!result?.has_next || loading" :aria-label="t('relation.nextLinkedPage', { resource: targetName })" @click="page++">{{ t('relation.next') }}</button>
        </div>
      </footer>
    </template>

    <RelationRemoveDialog
      v-if="removeTarget"
      :parent-name="parentName"
      :target-name="targetName"
      :target-label="removeTargetLabel"
      :junction-name="junctionName"
      :junction-key="removeTargetKey"
      :pending="removing"
      :error-message="mutationError"
      @cancel="closeRemoveDialog"
      @confirm="removeRelation"
    />
  </section>
</template>
