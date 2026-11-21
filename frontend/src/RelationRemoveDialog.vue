<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import BaseModal from './BaseModal.vue'

const props = defineProps<{
  parentName: string
  targetName: string
  targetLabel: string
  junctionName: string
  junctionKey: string
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
    :title="t('relation.removeRelationTitle')"
    :label="t('relation.removeDialogLabel')"
    :pending="pending"
    dialog-class="delete-dialog relation-remove-dialog"
    size="small"
    @close="cancel"
  >
    <p id="relation-remove-description">
      {{ t('relation.removeRelationMessage', { parent: parentName, target: `${targetName} (${targetLabel})` }) }}
    </p>
    <p id="relation-remove-preservation">{{ t('relation.removeRelationPreserves') }}</p>
    <div class="delete-record-identity">
      <span>{{ t('relation.junctionRecordKey', { resource: junctionName }) }}</span>
      <code>{{ junctionKey }}</code>
    </div>
    <p v-if="errorMessage" class="delete-error" role="alert">{{ t(errorMessage) }}</p>
    <template #footer>
      <button data-autofocus type="button" class="secondary-button" :disabled="pending" @click="cancel">
        {{ t('common.cancel') }}
      </button>
      <button type="button" class="danger-button" :disabled="pending" :aria-busy="pending" @click="emit('confirm')">
        {{ pending ? t('relation.removingRelation') : t('relation.confirmRemoveRelation') }}
      </button>
    </template>
  </BaseModal>
</template>
