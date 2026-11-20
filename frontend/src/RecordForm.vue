<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { can } from './auth'
import { formatRecordValue, type RecordRow } from './records'
import {
  buildCreatePayload,
  buildUpdatePayload,
  cloneFormState,
  initialFormState,
  isFormDirty,
  validateRecordForm,
  type FormFieldState,
  type FormMode,
  type RecordFormState,
} from './recordForm'
import { buildRecordKey, createRecord, MutationRequestError, updateRecord } from './mutations'
import type { ColumnMetadata, ForeignKeyMetadata, ResourceMetadata, SchemaMetadata } from './schema'
import { findReferencedResource } from './relation'
import RelationSelector from './RelationSelector.vue'
import ReverseRelationPanel from './ReverseRelationPanel.vue'
import ManyToManyPanel from './ManyToManyPanel.vue'
import { readableReverseRelations } from './relatedRecords'
import { readableManyToManyRelations } from './manyToMany'
import { errorTranslationKey, fieldErrorTranslationKey } from './i18n'

interface RelationView {
  foreignKey: ForeignKeyMetadata
  resource: ResourceMetadata
  columns: ColumnMetadata[]
}

type FormEntry =
  | { key: string; kind: 'relation'; relation: RelationView }
  | { key: string; kind: 'column'; column: ColumnMetadata }

const props = withDefaults(defineProps<{
  backendBaseURL: string
  schemaName: string
  resource: ResourceMetadata
  schemas?: SchemaMetadata[]
  mode: FormMode
  record?: RecordRow
  updateAllowed?: boolean
  writeEnabled?: boolean
}>(), { updateAllowed: true, writeEnabled: false })
const emit = defineEmits<{
  saved: [record: RecordRow]
  'audit-failed': [record: RecordRow | null]
  cancel: []
  'dirty-change': [dirty: boolean]
}>()

const { t, locale } = useI18n()
const state = ref<RecordFormState>({})
const relationSelections = ref<Record<string, boolean>>({})
let initial: RecordFormState = {}
const saving = ref(false)
const serverError = ref('')
const serverErrorCode = ref('')
const serverErrorStatus = ref(0)
const fieldErrors = ref<Record<string, string>>({})

const dirty = computed(() => isFormDirty(state.value, initial))
const formReadOnly = computed(() => props.mode === 'edit' && props.updateAllowed === false)

const resolvedRelations = computed(() => {
  const relations: RelationView[] = []
  for (const foreignKey of props.resource.foreign_keys) {
    const columns = relationColumns(foreignKey)
    if (!columns || columns.some(isReadOnly)) continue
    const resource = findReferencedResource(
      props.schemas ?? [],
      foreignKey.referenced_schema,
      foreignKey.referenced_table,
    )
    if (!resource || !can(foreignKey.referenced_schema, foreignKey.referenced_table, 'read')) continue
    relations.push({ foreignKey, resource, columns })
  }
  return relations
})

const manyToManyRelations = computed(() => readableManyToManyRelations(
  props.schemaName,
  props.resource,
  props.schemas ?? [],
))
const reverseRelations = computed(() => {
  const junctionRelations = new Set(manyToManyRelations.value.map((relation) =>
    `${relation.junctionSchema}.${relation.junctionResource.name}.${relation.parentForeignKey.name}`,
  ))
  return readableReverseRelations(props.resource, props.schemas ?? []).filter((item) =>
    !junctionRelations.has(`${item.relation.schema}.${item.relation.resource}.${item.relation.name}`),
  )
})

const formEntries = computed<FormEntry[]>(() => {
  const byFirstColumn = new Map(resolvedRelations.value.map((relation) => [relation.foreignKey.columns[0], relation]))
  const groupedColumns = new Set(resolvedRelations.value.flatMap((relation) => relation.foreignKey.columns.slice(1)))
  const entries: FormEntry[] = []
  for (const column of props.resource.columns) {
    const relation = byFirstColumn.get(column.name)
    if (relation) entries.push({ key: `relation-${relation.foreignKey.name}`, kind: 'relation', relation })
    else if (!groupedColumns.has(column.name)) entries.push({ key: column.name, kind: 'column', column })
  }
  return entries
})

function resetForm() {
  initial = initialFormState(props.mode, props.resource, props.record)
  state.value = cloneFormState(initial)
  const selections: Record<string, boolean> = {}
  for (const foreignKey of props.resource.foreign_keys) {
    selections[foreignKey.name] = props.mode === 'edit' && foreignKey.columns.some((column) => {
      const value = props.record?.[column]
      return value !== null && value !== undefined
    })
  }
  relationSelections.value = selections
  serverError.value = ''
  serverErrorCode.value = ''
  serverErrorStatus.value = 0
  fieldErrors.value = {}
}

function relationColumns(foreignKey: ForeignKeyMetadata): ColumnMetadata[] | null {
  if (foreignKey.columns.length === 0 || foreignKey.columns.length !== foreignKey.referenced_columns.length) return null
  const columns = foreignKey.columns.map((name) => props.resource.columns.find((column) => column.name === name))
  return columns.every((column): column is ColumnMetadata => Boolean(column)) ? columns : null
}

function unavailableRelation(columnName: string): ForeignKeyMetadata | null {
  const foreignKey = props.resource.foreign_keys.find((relation) =>
    relation.columns[0] === columnName && relationColumns(relation),
  )
  if (!foreignKey || resolvedRelations.value.some((relation) => relation.foreignKey.name === foreignKey.name)) return null
  const resource = findReferencedResource(
    props.schemas ?? [],
    foreignKey.referenced_schema,
    foreignKey.referenced_table,
  )
  return !resource || !can(foreignKey.referenced_schema, foreignKey.referenced_table, 'read') ? foreignKey : null
}

function inputType(column: ColumnMetadata): string {
  return baseType(column.data_type) === 'date' ? 'date' : 'text'
}

function inputPlaceholder(column: ColumnMetadata): string {
  const type = baseType(column.data_type)
  if (type === 'timestamp with time zone') return '2025-01-02T03:04:05Z'
  if (type === 'timestamp without time zone') return '2025-01-02T03:04:05.123'
  if (type === 'uuid') return '00112233-4455-6677-8899-aabbccddeeff'
  if (['bigint', 'numeric', 'decimal'].includes(type)) return t('form.exactNumeric')
  return ''
}

function isReadOnly(column: ColumnMetadata): boolean {
  return formReadOnly.value || !column.editable || (props.mode === 'edit' && column.primary_key)
}

function readOnlyText(column: ColumnMetadata): string {
  if (formReadOnly.value) return formatRecordValue(props.record?.[column.name], column.data_type, locale.value)
  if (column.generated) return t('form.generated')
  if (column.identity === 'always') return t('form.generatedIdentity')
  if (column.primary_key && props.mode === 'edit') return formatRecordValue(props.record?.[column.name], column.data_type, locale.value)
  return column.editable ? '' : t('form.unsupportedType')
}

function editField(field: FormFieldState) {
  field.useDefault = false
  field.isNull = false
}

function relationRequired(relation: RelationView): boolean {
  return relation.columns.some((column) => !column.nullable)
}

function relationNullable(relation: RelationView): boolean {
  return relation.columns.every((column) => column.nullable)
}

function relationCanUseDefault(relation: RelationView): boolean {
  return props.mode === 'create' && relation.columns.every((column) =>
    column.default !== null || column.identity !== '' || column.generated,
  )
}

function relationUsesDefault(relation: RelationView): boolean {
  return relation.foreignKey.columns.every((column) => state.value[column]?.useDefault)
}

function toggleRelationDefault(relation: RelationView, event: Event) {
  const useDefault = (event.target as HTMLInputElement).checked
  for (const column of relation.foreignKey.columns) state.value[column].useDefault = useDefault
  if (useDefault) relationSelections.value[relation.foreignKey.name] = false
}

function relationSelectedValues(relation: RelationView): unknown[] {
  return relation.foreignKey.columns.map((column) => {
    const field = state.value[column]
    return field.isNull || field.useDefault ? null : field.value
  })
}

function selectRelation(relation: RelationView, record: RecordRow) {
  relation.foreignKey.columns.forEach((localColumn, index) => {
    const value = record[relation.foreignKey.referenced_columns[index]]
    const field = state.value[localColumn]
    field.value = value !== null && typeof value === 'object' ? JSON.stringify(value) : value === null ? '' : String(value)
    field.isNull = value === null
    field.useDefault = false
  })
  relationSelections.value[relation.foreignKey.name] = true
}

function clearRelation(relation: RelationView) {
  if (!relationNullable(relation)) return
  for (const column of relation.foreignKey.columns) {
    const field = state.value[column]
    field.value = ''
    field.isNull = true
    field.useDefault = false
  }
  relationSelections.value[relation.foreignKey.name] = false
}

function relationError(relation: RelationView): string {
  for (const column of relation.foreignKey.columns) {
    if (fieldErrors.value[column]) return fieldErrors.value[column]
  }
  return ''
}

async function save() {
  if (formReadOnly.value) return
  serverError.value = ''
  serverErrorCode.value = ''
  serverErrorStatus.value = 0
  fieldErrors.value = validateRecordForm(props.mode, props.resource, state.value)
  if (Object.keys(fieldErrors.value).length > 0) {
    const firstInvalid = props.resource.columns.find((column) => fieldErrors.value[column.name])
    await nextTick()
    if (firstInvalid) document.getElementById(`field-${firstInvalid.name}`)?.focus()
    return
  }

  saving.value = true
  try {
    const record = props.mode === 'create'
      ? await createRecord(props.backendBaseURL, props.schemaName, props.resource.name, buildCreatePayload(props.resource, state.value))
      : await updateRecord(
          props.backendBaseURL,
          props.schemaName,
          props.resource.name,
          buildRecordKey(props.resource, props.record ?? {}),
          buildUpdatePayload(props.resource, state.value, initial),
        )
    initial = cloneFormState(state.value)
    emit('saved', record)
  } catch (error) {
    if (error instanceof MutationRequestError && error.mutationCommitted) {
      emit('audit-failed', error.record)
    } else if (error instanceof MutationRequestError) {
      serverError.value = errorTranslationKey(error.code, error.status)
      serverErrorCode.value = error.code
      serverErrorStatus.value = error.status
      fieldErrors.value = Object.fromEntries(
        Object.keys(error.fieldErrors).map((field) => [field, fieldErrorTranslationKey(error.code)]),
      )
    } else {
      serverError.value = 'errors.generic'
    }
  } finally {
    saving.value = false
  }
}

function cancel() {
  if (dirty.value && !window.confirm(t('records.confirmDiscard'))) return
  emit('cancel')
}

function beforeUnload(event: BeforeUnloadEvent) {
  if (!dirty.value) return
  event.preventDefault()
  event.returnValue = ''
}

watch(() => [props.mode, props.resource, props.record], resetForm, { immediate: true })
watch(dirty, (value) => emit('dirty-change', value), { immediate: true })
onMounted(() => window.addEventListener('beforeunload', beforeUnload))
onBeforeUnmount(() => {
  window.removeEventListener('beforeunload', beforeUnload)
  emit('dirty-change', false)
})

function baseType(dataType: string): string {
  return dataType.toLowerCase().trim().replace(/\([^)]*\)/g, '').trim()
}
</script>

<template>
  <section class="record-form-panel" :aria-label="t(mode === 'create' ? 'records.create' : formReadOnly ? 'common.view' : 'records.edit')">
    <div class="record-form-heading">
      <div>
        <p class="eyebrow">{{ t(mode === 'create' ? 'records.new' : formReadOnly ? 'common.view' : 'records.edit') }}</p>
        <h3>{{ resource.name }}</h3>
      </div>
      <button type="button" class="form-close" :aria-label="t('form.close')" @click="cancel">×</button>
    </div>

    <div v-if="serverError" class="form-server-error" role="alert">
      <strong>{{ t(serverErrorStatus === 403 ? 'errors.forbidden' : 'form.saveFailed') }}</strong>
      <p>{{ t(serverError) }}</p>
      <code v-if="serverErrorCode">{{ serverErrorCode }}</code>
    </div>

    <form class="record-form" @submit.prevent="save">
      <template v-for="entry in formEntries" :key="entry.key">
        <div v-if="entry.kind === 'relation'" class="record-form-field relation-form-field">
          <div class="field-label">
            <label :for="`field-${entry.relation.foreignKey.columns[0]}`">{{ entry.relation.foreignKey.columns.join(' + ') }}</label>
            <code>{{ entry.relation.columns.map((column) => column.data_type).join(' · ') }}</code>
            <span v-if="relationRequired(entry.relation) && !relationUsesDefault(entry.relation)" class="field-required">{{ t('form.required') }}</span>
          </div>
          <div v-if="relationCanUseDefault(entry.relation)" class="field-options">
            <label>
              <input
                type="checkbox"
                :checked="relationUsesDefault(entry.relation)"
                @change="toggleRelationDefault(entry.relation, $event)"
              />
              {{ t('form.useDefault') }}
            </label>
          </div>
          <RelationSelector
            :backend-base-u-r-l="backendBaseURL"
            :schema-name="entry.relation.foreignKey.referenced_schema"
            :resource="entry.relation.resource"
            :referenced-columns="entry.relation.foreignKey.referenced_columns"
            :selected-values="relationSelectedValues(entry.relation)"
            :has-selection="relationSelections[entry.relation.foreignKey.name] ?? false"
            :input-id="`field-${entry.relation.foreignKey.columns[0]}`"
            :label="entry.relation.foreignKey.columns.join(' + ')"
            :nullable="relationNullable(entry.relation)"
            :required="relationRequired(entry.relation)"
            :disabled="saving || relationUsesDefault(entry.relation)"
            @select="selectRelation(entry.relation, $event)"
            @clear="clearRelation(entry.relation)"
          />
          <p class="relation-target">
            {{ t('relation.target', { resource: `${entry.relation.foreignKey.referenced_schema}.${entry.relation.foreignKey.referenced_table}` }) }}
          </p>
          <p v-if="relationError(entry.relation)" class="field-error" role="alert">{{ t(relationError(entry.relation)) }}</p>
        </div>

        <div v-else class="record-form-field">
          <div class="field-label">
            <label :for="`field-${entry.column.name}`">{{ entry.column.name }}</label>
            <code>{{ entry.column.data_type }}</code>
            <span v-if="entry.column.primary_key" class="field-badge">PK</span>
            <span v-if="!entry.column.nullable && !isReadOnly(entry.column) && !state[entry.column.name].useDefault" class="field-required">{{ t('form.required') }}</span>
          </div>

          <template v-if="isReadOnly(entry.column)">
            <input :id="`field-${entry.column.name}`" :value="readOnlyText(entry.column)" disabled />
          </template>
          <template v-else>
            <div v-if="mode === 'create' && (entry.column.default !== null || entry.column.identity !== '' || entry.column.generated)" class="field-options">
              <label>
                <input v-model="state[entry.column.name].useDefault" type="checkbox" />
                {{ t('form.useDefault') }}
              </label>
            </div>
            <div v-if="entry.column.nullable && !state[entry.column.name].useDefault" class="field-options">
              <label>
                <input v-model="state[entry.column.name].isNull" type="checkbox" @change="state[entry.column.name].useDefault = false" />
                {{ t('form.null') }}
              </label>
            </div>

            <select
              v-if="entry.column.enum_values.length > 0"
              :id="`field-${entry.column.name}`"
              v-model="state[entry.column.name].value"
              :disabled="saving || state[entry.column.name].isNull || state[entry.column.name].useDefault"
              @change="editField(state[entry.column.name])"
            >
              <option value="" disabled>{{ t('form.chooseValue') }}</option>
              <option v-for="value in entry.column.enum_values" :key="value" :value="value">{{ value }}</option>
            </select>
            <select
              v-else-if="baseType(entry.column.data_type) === 'boolean'"
              :id="`field-${entry.column.name}`"
              v-model="state[entry.column.name].value"
              :disabled="saving || state[entry.column.name].isNull || state[entry.column.name].useDefault"
              @change="editField(state[entry.column.name])"
            >
              <option value="true">true</option>
              <option value="false">false</option>
            </select>
            <textarea
              v-else-if="['json', 'jsonb'].includes(baseType(entry.column.data_type))"
              :id="`field-${entry.column.name}`"
              v-model="state[entry.column.name].value"
              rows="5"
              :disabled="saving || state[entry.column.name].isNull || state[entry.column.name].useDefault"
              @input="editField(state[entry.column.name])"
            />
            <input
              v-else
              :id="`field-${entry.column.name}`"
              v-model="state[entry.column.name].value"
              :type="inputType(entry.column)"
              :placeholder="inputPlaceholder(entry.column)"
              :disabled="saving || state[entry.column.name].isNull || state[entry.column.name].useDefault"
              autocomplete="off"
              @input="editField(state[entry.column.name])"
            />
          </template>
          <p v-if="unavailableRelation(entry.column.name)" class="relation-unavailable">{{ t('relation.unavailable') }}</p>
          <p v-if="fieldErrors[entry.column.name]" class="field-error" role="alert">{{ t(fieldErrors[entry.column.name]) }}</p>
        </div>
      </template>

      <div class="record-form-actions">
        <button v-if="!formReadOnly" type="submit" class="primary-button" :disabled="saving || (mode === 'edit' && !dirty)">
          {{ saving ? t('records.saving') : t(mode === 'create' ? 'records.create' : 'records.save') }}
        </button>
        <button type="button" class="secondary-button" :disabled="saving" @click="cancel">{{ t(formReadOnly ? 'common.close' : 'common.cancel') }}</button>
        <span v-if="dirty" class="unsaved-label">{{ t('form.unsaved') }}</span>
      </div>
    </form>

    <section v-if="reverseRelations.length || manyToManyRelations.length" class="reverse-relations" :aria-label="t('relation.relatedRecords')">
      <div class="reverse-relations-heading">
        <p class="eyebrow">{{ t('relation.relatedRecords') }}</p>
        <h3>{{ t('relation.relatedRecords') }}</h3>
      </div>
      <p v-if="mode === 'create'" class="reverse-relation-save-first" role="status">
        {{ t('relation.saveRecordFirst') }}
      </p>
      <template v-else>
        <ManyToManyPanel
          v-for="relation in manyToManyRelations"
          :key="relation.id"
          :backend-base-u-r-l="backendBaseURL"
          :parent-name="`${schemaName}.${resource.name}`"
          :relation="relation"
          :schemas="schemas ?? []"
          :parent-record="record"
          :write-enabled="writeEnabled"
        />
        <ReverseRelationPanel
          v-for="item in reverseRelations"
          :key="`${item.relation.schema}.${item.relation.resource}.${item.relation.name}`"
          :backend-base-u-r-l="backendBaseURL"
          :relation="item.relation"
          :child-resource="item.childResource"
          :parent-record="record"
        />
      </template>
    </section>
  </section>
</template>
