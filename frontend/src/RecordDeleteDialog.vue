<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  resourceName: string
  recordKey: string
  pending: boolean
  errorMessage: string
}>()
const emit = defineEmits<{ cancel: []; confirm: [] }>()
const { t } = useI18n()
const dialog = ref<HTMLElement | null>(null)
const cancelButton = ref<HTMLButtonElement | null>(null)
let previousFocus: HTMLElement | null = null

function cancel() {
  if (!props.pending) emit('cancel')
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    event.preventDefault()
    cancel()
    return
  }
  if (event.key !== 'Tab') return
  const buttons = Array.from(dialog.value?.querySelectorAll<HTMLButtonElement>('button:not([disabled])') ?? [])
  if (buttons.length === 0) {
    event.preventDefault()
    dialog.value?.focus()
    return
  }
  const first = buttons[0]
  const last = buttons[buttons.length - 1]
  if (event.shiftKey && (document.activeElement === first || document.activeElement === dialog.value)) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && (document.activeElement === last || document.activeElement === dialog.value)) {
    event.preventDefault()
    first.focus()
  }
}

onMounted(async () => {
  previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
  await nextTick()
  cancelButton.value?.focus()
})
onBeforeUnmount(() => previousFocus?.focus())
</script>

<template>
  <div class="dialog-backdrop" @click.self="cancel">
    <section
      ref="dialog"
      class="delete-dialog"
      role="dialog"
      aria-modal="true"
      :aria-label="t('records.delete.dialogLabel')"
      aria-labelledby="delete-dialog-title"
      aria-describedby="delete-dialog-description delete-dialog-warning"
      tabindex="-1"
      @keydown="onKeydown"
    >
      <h3 id="delete-dialog-title">{{ t('records.delete.title') }}</h3>
      <p id="delete-dialog-description">{{ t('records.delete.message', { resource: resourceName }) }}</p>
      <div class="delete-record-identity">
        <span>{{ t('records.delete.identifier') }}</span>
        <code>{{ recordKey }}</code>
      </div>
      <p id="delete-dialog-warning" class="delete-warning">{{ t('records.delete.irreversible') }}</p>
      <p v-if="errorMessage" class="delete-error" role="alert">{{ t(errorMessage) }}</p>
      <div class="delete-dialog-actions">
        <button ref="cancelButton" type="button" class="secondary-button" :disabled="pending" @click="cancel">
          {{ t('common.cancel') }}
        </button>
        <button type="button" class="danger-button" :disabled="pending" :aria-busy="pending" @click="emit('confirm')">
          {{ pending ? t('records.delete.deleting') : t('records.delete.confirm') }}
        </button>
      </div>
    </section>
  </div>
</template>
