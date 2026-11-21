<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'

const modalStack: symbol[] = []
let bodyOverflow = ''

const props = withDefaults(defineProps<{
  title: string
  label?: string
  pending?: boolean
  size?: 'small' | 'medium' | 'wide'
  dialogClass?: string
}>(), { label: '', pending: false, size: 'medium', dialogClass: '' })
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const dialog = ref<HTMLElement | null>(null)
const titleID = `modal-title-${useId()}`
const modalID = Symbol('modal')
let previousFocus: HTMLElement | null = null

function close() {
  if (!props.pending) emit('close')
}

function focusableElements(): HTMLElement[] {
  return Array.from(dialog.value?.querySelectorAll<HTMLElement>(
    'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])',
  ) ?? []).filter((element) => element.getAttribute('aria-hidden') !== 'true')
}

function focusFirst() {
  const preferred = dialog.value?.querySelector<HTMLElement>('[data-autofocus]')
  ;(preferred ?? focusableElements()[0] ?? dialog.value)?.focus()
}

function onKeydown(event: KeyboardEvent) {
  if (modalStack.at(-1) !== modalID) return
  if (event.key === 'Escape') {
    event.preventDefault()
    event.stopPropagation()
    close()
    return
  }
  if (event.key !== 'Tab') return
  const elements = focusableElements()
  if (elements.length === 0) {
    event.preventDefault()
    dialog.value?.focus()
    return
  }
  const first = elements[0]
  const last = elements[elements.length - 1]
  if (event.shiftKey && (document.activeElement === first || document.activeElement === dialog.value || !dialog.value?.contains(document.activeElement))) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && (document.activeElement === last || document.activeElement === dialog.value || !dialog.value?.contains(document.activeElement))) {
    event.preventDefault()
    first.focus()
  }
}

function onFocusIn(event: FocusEvent) {
  if (modalStack.at(-1) !== modalID || !(event.target instanceof Node) || dialog.value?.contains(event.target)) return
  focusFirst()
}

onMounted(async () => {
  previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
  if (modalStack.length === 0) bodyOverflow = document.body.style.overflow
  document.body.style.overflow = 'hidden'
  modalStack.push(modalID)
  document.addEventListener('keydown', onKeydown, true)
  document.addEventListener('focusin', onFocusIn)
  await nextTick()
  if (modalStack.at(-1) === modalID) focusFirst()
})

onBeforeUnmount(() => {
  const index = modalStack.lastIndexOf(modalID)
  if (index >= 0) modalStack.splice(index, 1)
  document.removeEventListener('keydown', onKeydown, true)
  document.removeEventListener('focusin', onFocusIn)
  if (modalStack.length === 0) document.body.style.overflow = bodyOverflow
  if (previousFocus?.isConnected) previousFocus.focus()
})
</script>

<template>
  <Teleport to="body">
    <div class="dialog-backdrop" @mousedown.self="close">
      <section
        ref="dialog"
        class="base-modal"
        :class="[`modal-${size}`, dialogClass]"
        role="dialog"
        aria-modal="true"
        :aria-label="label || undefined"
        :aria-labelledby="titleID"
        tabindex="-1"
      >
        <header class="modal-header">
          <div class="modal-heading">
            <slot name="eyebrow" />
            <h2 :id="titleID">{{ title }}</h2>
            <slot name="subtitle" />
          </div>
          <button type="button" class="modal-close" :disabled="pending" :aria-label="t('common.close')" @click="close">
            <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true"><path d="m6 6 12 12M18 6 6 18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" /></svg>
          </button>
        </header>
        <div class="modal-body"><slot /></div>
        <footer v-if="$slots.footer" class="modal-footer"><slot name="footer" /></footer>
      </section>
    </div>
  </Teleport>
</template>
