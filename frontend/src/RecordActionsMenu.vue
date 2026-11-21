<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  canEdit: boolean
  canView: boolean
  canHistory: boolean
  canDelete: boolean
}>()
const emit = defineEmits<{ edit: []; history: []; delete: [] }>()
const { t } = useI18n()
const trigger = ref<HTMLButtonElement | null>(null)
const menu = ref<HTMLElement | null>(null)
const open = ref(false)
const menuStyle = ref<Record<string, string>>({})
const menuID = `record-actions-${Math.random().toString(36).slice(2)}`
const instanceID = Symbol('record-actions')
const hasActions = () => props.canEdit || props.canView || props.canHistory || props.canDelete

function menuItems(): HTMLButtonElement[] {
  return Array.from(menu.value?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') ?? [])
}

function updatePosition() {
  const rect = trigger.value?.getBoundingClientRect()
  if (!rect) return
  const viewportWidth = window.innerWidth || 1024
  const viewportHeight = window.innerHeight || 768
  const width = Math.min(220, viewportWidth - 16)
  const height = menu.value?.offsetHeight ?? 180
  const left = Math.max(8, Math.min(rect.right - width, viewportWidth - width - 8))
  const below = viewportHeight - rect.bottom - 8
  const useAbove = below < height + 8 && rect.top > below
  menuStyle.value = {
    left: `${left}px`,
    width: `${width}px`,
    ...(useAbove
      ? { bottom: `${viewportHeight - rect.top + 4}px`, top: 'auto' }
      : { top: `${rect.bottom + 4}px`, bottom: 'auto' }),
    maxHeight: `${Math.max(80, Math.min(height, useAbove ? rect.top - 12 : below))}px`,
  }
}

async function openMenu(last = false) {
  if (!hasActions()) return
  window.dispatchEvent(new CustomEvent('velmir:record-menu-open', { detail: instanceID }))
  open.value = true
  await nextTick()
  updatePosition()
  const items = menuItems()
  ;(last ? items.at(-1) : items[0])?.focus()
}

function closeMenu(restoreFocus = false) {
  if (!open.value) return
  open.value = false
  if (restoreFocus) void nextTick(() => trigger.value?.focus())
}

function toggleMenu() {
  if (open.value) closeMenu(true)
  else void openMenu()
}

function onTriggerKeydown(event: KeyboardEvent) {
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    void openMenu(event.key === 'ArrowUp')
  } else if (event.key === 'Escape') closeMenu(true)
}

function onMenuKeydown(event: KeyboardEvent) {
  const items = menuItems()
  const index = items.indexOf(document.activeElement as HTMLButtonElement)
  if (event.key === 'Escape') {
    event.preventDefault()
    event.stopPropagation()
    closeMenu(true)
  } else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    const direction = event.key === 'ArrowDown' ? 1 : -1
    items[(index + direction + items.length) % items.length]?.focus()
  } else if (event.key === 'Home' || event.key === 'End') {
    event.preventDefault()
    ;(event.key === 'Home' ? items[0] : items.at(-1))?.focus()
  } else if (event.key === 'Tab') closeMenu()
}

function select(action: 'edit' | 'history' | 'delete') {
  closeMenu(true)
  void nextTick(() => {
    if (action === 'edit') emit('edit')
    else if (action === 'history') emit('history')
    else emit('delete')
  })
}

function onOutsidePointer(event: PointerEvent) {
  if (!(event.target instanceof Node)) return
  if (!trigger.value?.contains(event.target) && !menu.value?.contains(event.target)) closeMenu()
}

function onOtherMenu(event: Event) {
  if ((event as CustomEvent<symbol>).detail !== instanceID) closeMenu()
}

function onViewportChange() {
  closeMenu()
}

onMounted(() => {
  document.addEventListener('pointerdown', onOutsidePointer)
  window.addEventListener('velmir:record-menu-open', onOtherMenu)
  window.addEventListener('resize', onViewportChange)
  window.addEventListener('scroll', onViewportChange, true)
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onOutsidePointer)
  window.removeEventListener('velmir:record-menu-open', onOtherMenu)
  window.removeEventListener('resize', onViewportChange)
  window.removeEventListener('scroll', onViewportChange, true)
})
</script>

<template>
  <div class="record-actions">
    <button
      v-if="hasActions()"
      ref="trigger"
      type="button"
      class="record-actions-trigger"
      :aria-label="t('records.recordActions')"
      aria-haspopup="menu"
      :aria-expanded="open"
      :aria-controls="open ? menuID : undefined"
      @click="toggleMenu"
      @keydown="onTriggerKeydown"
    ><span aria-hidden="true">⋯</span></button>
    <span v-else class="record-actions-empty" :aria-label="t('records.noActions')">—</span>
    <Teleport to="body">
      <div
        v-if="open"
        :id="menuID"
        ref="menu"
        class="record-actions-menu"
        :style="menuStyle"
        role="menu"
        :aria-label="t('records.recordActions')"
        @keydown="onMenuKeydown"
      >
        <button v-if="canEdit || canView" type="button" role="menuitem" @click="select('edit')">{{ t(canEdit ? 'common.edit' : 'common.view') }}</button>
        <button v-if="canHistory" type="button" role="menuitem" @click="select('history')">{{ t('history.open') }}</button>
        <div v-if="canDelete && (canEdit || canView || canHistory)" class="menu-separator" role="separator" />
        <button v-if="canDelete" type="button" role="menuitem" class="danger-menu-item record-delete-button" @click="select('delete')">{{ t('records.delete.action') }}</button>
      </div>
    </Teleport>
  </div>
</template>
