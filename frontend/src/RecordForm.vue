<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
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
import type { ColumnMetadata, ResourceMetadata } from './schema'
import { errorTranslationKey, fieldErrorTranslationKey } from './i18n'

const props = defineProps<{
  backendBaseURL: string
  schemaName: string
  resource: ResourceMetadata
  mode: FormMode
  record?: RecordRow
}>()
const emit = defineEmits<{
  saved: [record: RecordRow]
  cancel: []
  'dirty-change': [dirty: boolean]
}>()

const { t, locale } = useI18n()
const state = ref<RecordFormState>({})
let initial: RecordFormState = {}
const saving = ref(false)
const serverError = ref('')
const serverErrorCode = ref('')
const serverErrorStatus = ref(0)
const fieldErrors = ref<Record<string, string>>({})

const dirty = computed(() => isFormDirty(state.value, initial))

function resetForm() {
  initial = initialFormState(props.mode, props.resource, props.record)
  state.value = cloneFormState(initial)
  serverError.value = ''
  serverErrorCode.value = ''
  serverErrorStatus.value = 0
  fieldErrors.value = {}
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
  return !column.editable || (props.mode === 'edit' && column.primary_key)
}

function readOnlyText(column: ColumnMetadata): string {
  if (column.generated) return t('form.generated')
  if (column.identity === 'always') return t('form.generatedIdentity')
  if (column.primary_key && props.mode === 'edit') return formatRecordValue(props.record?.[column.name], column.data_type, locale.value)
  return column.editable ? '' : t('form.unsupportedType')
}

function editField(field: FormFieldState) {
  field.useDefault = false
  field.isNull = false
}

async function save() {
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
    if (error instanceof MutationRequestError) {
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
  <section class="record-form-panel" :aria-label="t(mode === 'create' ? 'records.create' : 'records.edit')">
    <div class="record-form-heading">
      <div>
        <p class="eyebrow">{{ t(mode === 'create' ? 'records.new' : 'records.edit') }}</p>
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
      <div v-for="column in resource.columns" :key="column.name" class="record-form-field">
        <div class="field-label">
          <label :for="`field-${column.name}`">{{ column.name }}</label>
          <code>{{ column.data_type }}</code>
          <span v-if="column.primary_key" class="field-badge">PK</span>
          <span v-if="!column.nullable && !isReadOnly(column) && !state[column.name].useDefault" class="field-required">{{ t('form.required') }}</span>
        </div>

        <template v-if="isReadOnly(column)">
          <input :id="`field-${column.name}`" :value="readOnlyText(column)" disabled />
        </template>
        <template v-else>
          <div v-if="mode === 'create' && (column.default !== null || column.identity !== '' || column.generated)" class="field-options">
            <label>
              <input v-model="state[column.name].useDefault" type="checkbox" />
              {{ t('form.useDefault') }}
            </label>
          </div>
          <div v-if="column.nullable && !state[column.name].useDefault" class="field-options">
            <label>
              <input v-model="state[column.name].isNull" type="checkbox" @change="state[column.name].useDefault = false" />
              {{ t('form.null') }}
            </label>
          </div>

          <select
            v-if="column.enum_values.length > 0"
            :id="`field-${column.name}`"
            v-model="state[column.name].value"
            :disabled="saving || state[column.name].isNull || state[column.name].useDefault"
            @change="editField(state[column.name])"
          >
            <option value="" disabled>{{ t('form.chooseValue') }}</option>
            <option v-for="value in column.enum_values" :key="value" :value="value">{{ value }}</option>
          </select>
          <select
            v-else-if="baseType(column.data_type) === 'boolean'"
            :id="`field-${column.name}`"
            v-model="state[column.name].value"
            :disabled="saving || state[column.name].isNull || state[column.name].useDefault"
            @change="editField(state[column.name])"
          >
            <option value="true">true</option>
            <option value="false">false</option>
          </select>
          <textarea
            v-else-if="['json', 'jsonb'].includes(baseType(column.data_type))"
            :id="`field-${column.name}`"
            v-model="state[column.name].value"
            rows="5"
            :disabled="saving || state[column.name].isNull || state[column.name].useDefault"
            @input="editField(state[column.name])"
          />
          <input
            v-else
            :id="`field-${column.name}`"
            v-model="state[column.name].value"
            :type="inputType(column)"
            :placeholder="inputPlaceholder(column)"
            :disabled="saving || state[column.name].isNull || state[column.name].useDefault"
            autocomplete="off"
            @input="editField(state[column.name])"
          />
        </template>
        <p v-if="fieldErrors[column.name]" class="field-error" role="alert">{{ t(fieldErrors[column.name]) }}</p>
      </div>

      <div class="record-form-actions">
        <button type="submit" class="primary-button" :disabled="saving || (mode === 'edit' && !dirty)">
          {{ saving ? t('records.saving') : t(mode === 'create' ? 'records.create' : 'records.save') }}
        </button>
        <button type="button" class="secondary-button" :disabled="saving" @click="cancel">{{ t('common.cancel') }}</button>
        <span v-if="dirty" class="unsaved-label">{{ t('form.unsaved') }}</span>
      </div>
    </form>
  </section>
</template>
