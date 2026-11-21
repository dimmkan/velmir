<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import BaseModal from './BaseModal.vue'
import RecordHistory from './RecordHistory.vue'
import type { ResourceMetadata } from './schema'

defineProps<{
  backendBaseURL: string
  schemaName: string
  resource: ResourceMetadata
  recordKey: string
}>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
</script>

<template>
  <BaseModal :title="t('history.title')" size="wide" @close="emit('close')">
    <template #eyebrow><p class="eyebrow">{{ schemaName }}.{{ resource.name }}</p></template>
    <template #subtitle><code class="history-record-key">{{ recordKey }}</code></template>
    <RecordHistory
      embedded
      :backend-base-u-r-l="backendBaseURL"
      :schema-name="schemaName"
      :resource="resource"
      :record-key="recordKey"
    />
    <template #footer>
      <span class="modal-footer-spacer" />
      <button type="button" class="secondary-button" @click="emit('close')">{{ t('common.close') }}</button>
    </template>
  </BaseModal>
</template>
