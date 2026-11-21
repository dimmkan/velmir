<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseModal from './BaseModal.vue'
import RecordForm from './RecordForm.vue'
import type { RecordRow } from './records'
import type { FormMode } from './recordForm'
import type { ResourceMetadata, SchemaMetadata } from './schema'

const props = defineProps<{
  backendBaseURL: string
  schemaName: string
  resource: ResourceMetadata
  schemas: SchemaMetadata[]
  mode: FormMode
  record?: RecordRow
  updateAllowed: boolean
  writeEnabled: boolean
}>()
const emit = defineEmits<{
  saved: [record: RecordRow]
  'audit-failed': [record: RecordRow | null]
  close: []
  'dirty-change': [dirty: boolean]
}>()
const { t } = useI18n()
const form = ref<InstanceType<typeof RecordForm> | null>(null)
const state = reactive({ dirty: false, saving: false, readOnly: false, createImpossible: false })

function close() {
  form.value?.cancel()
}
</script>

<template>
  <BaseModal
    :title="t(mode === 'create' ? 'records.new' : state.readOnly ? 'common.view' : 'records.edit')"
    :pending="state.saving"
    size="wide"
    @close="close"
  >
    <template #eyebrow><p class="eyebrow">{{ schemaName }}.{{ resource.name }}</p></template>
    <RecordForm
      ref="form"
      embedded
      :backend-base-u-r-l="props.backendBaseURL"
      :schema-name="schemaName"
      :resource="resource"
      :schemas="schemas"
      :mode="mode"
      :record="record"
      :update-allowed="updateAllowed"
      :write-enabled="writeEnabled"
      @saved="emit('saved', $event)"
      @audit-failed="emit('audit-failed', $event)"
      @cancel="emit('close')"
      @dirty-change="emit('dirty-change', $event)"
      @state-change="Object.assign(state, $event)"
    />
    <template #footer>
      <span v-if="state.dirty" class="unsaved-label">{{ t('form.unsaved') }}</span>
      <span class="modal-footer-spacer" />
      <button type="button" class="secondary-button" :disabled="state.saving" @click="close">{{ t(state.readOnly ? 'common.close' : 'common.cancel') }}</button>
      <button
        v-if="!state.readOnly && !state.createImpossible"
        type="button"
        class="primary-button record-form-submit"
        :disabled="state.saving || (mode === 'edit' && !state.dirty)"
        :aria-busy="state.saving"
        @click="form?.save()"
      >{{ state.saving ? t('records.saving') : t(mode === 'create' ? 'records.create' : 'records.save') }}</button>
    </template>
  </BaseModal>
</template>
