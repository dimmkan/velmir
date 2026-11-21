<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import BaseModal from './BaseModal.vue'

const props = defineProps<{
  resourceName: string
  recordKey: string
  pending: boolean
  errorMessage: string
}>()
const emit = defineEmits<{ cancel: []; confirm: [] }>()
const { t } = useI18n()

function cancel() {
  if (!props.pending) emit('cancel')
}
</script>

<template>
  <BaseModal
    :title="t('records.delete.title')"
    :label="t('records.delete.dialogLabel')"
    :pending="pending"
    dialog-class="delete-dialog"
    size="small"
    @close="cancel"
  >
    <p id="delete-dialog-description">{{ t('records.delete.message', { resource: resourceName }) }}</p>
    <div class="delete-record-identity">
      <span>{{ t('records.delete.identifier') }}</span>
      <code>{{ recordKey }}</code>
    </div>
    <p id="delete-dialog-warning" class="delete-warning">{{ t('records.delete.irreversible') }}</p>
    <p v-if="errorMessage" class="delete-error" role="alert">{{ t(errorMessage) }}</p>
    <template #footer>
      <button data-autofocus type="button" class="secondary-button" :disabled="pending" @click="cancel">
        {{ t('common.cancel') }}
      </button>
      <button type="button" class="danger-button" :disabled="pending" :aria-busy="pending" @click="emit('confirm')">
        {{ pending ? t('records.delete.deleting') : t('records.delete.confirm') }}
      </button>
    </template>
  </BaseModal>
</template>
