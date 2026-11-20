<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { defaultRecordsQuery, fetchRecords, type RecordRow } from './records'
import {
  exactRelationFilters,
  relationKeyLabel,
  relationOption,
  relationRecordKey,
  relationValuesKey,
  type RelationOption,
} from './relation'
import type { ResourceMetadata } from './schema'

const props = defineProps<{
  backendBaseURL: string
  schemaName: string
  resource: ResourceMetadata
  referencedColumns: string[]
  selectedValues: unknown[]
  hasSelection: boolean
  inputId: string
  label: string
  nullable: boolean
  required: boolean
  disabled: boolean
}>()
const emit = defineEmits<{
  select: [record: RecordRow]
  clear: []
}>()

const { t } = useI18n()
const root = ref<HTMLElement | null>(null)
const input = ref<HTMLInputElement | null>(null)
const dropdown = ref<HTMLElement | null>(null)
const isOpen = ref(false)
const query = ref('')
const loadedSearch = ref('')
const options = ref<RelationOption[]>([])
const selectedRecord = ref<RecordRow | null>(null)
const loading = ref(false)
const loadingMore = ref(false)
const loadError = ref(false)
const selectionLoadError = ref(false)
const hasNext = ref(false)
const page = ref(0)
const activeIndex = ref(-1)
const dropdownStyle = ref<Record<string, string>>({})

let searchTimer = 0
let optionsController: AbortController | null = null
let selectionController: AbortController | null = null
let optionsRequestID = 0
let selectionRequestID = 0

const selectedKey = computed(() => relationValuesKey(props.selectedValues))
const completeSelection = computed(() =>
  props.hasSelection &&
  props.selectedValues.length === props.referencedColumns.length &&
  props.selectedValues.every((value) => value !== null && value !== undefined),
)
const visibleOptions = computed(() => {
  const result = [...options.value]
  if (selectedRecord.value) {
    const option = relationOption(selectedRecord.value, props.resource, props.referencedColumns)
    const index = result.findIndex((item) => item.key === option.key)
    if (index >= 0) result.splice(index, 1)
    result.unshift(option)
  }
  return result
})
const selectedOption = computed(() => visibleOptions.value.find((option) => option.key === selectedKey.value))
const selectedText = computed(() => {
  if (!props.hasSelection) return ''
  if (selectedOption.value) {
    const { label, keyLabel } = selectedOption.value
    return label === keyLabel ? label : `${label} · ${keyLabel}`
  }
  const record = Object.fromEntries(props.referencedColumns.map((column, index) => [column, props.selectedValues[index]]))
  return relationKeyLabel(record, props.referencedColumns)
})
const listboxID = computed(() => `${props.inputId}-listbox`)
const activeDescendant = computed(() =>
  isOpen.value && activeIndex.value >= 0 ? `${listboxID.value}-option-${activeIndex.value}` : undefined,
)

function openDropdown() {
  if (props.disabled || isOpen.value) return
  isOpen.value = true
  query.value = ''
  activeIndex.value = visibleOptions.value.findIndex((option) => option.key === selectedKey.value)
  updatePosition()
  if (loadedSearch.value) void loadOptions(1, false)
}

function closeDropdown() {
  isOpen.value = false
  query.value = ''
  activeIndex.value = -1
  window.clearTimeout(searchTimer)
}

function handleInput(event: Event) {
  const value = (event.target as HTMLInputElement).value
  openDropdown()
  query.value = value
  window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(() => void loadOptions(1, false), 300)
}

async function loadOptions(nextPage: number, append: boolean) {
  if (props.disabled || append && (loadingMore.value || !hasNext.value)) return
  const requestID = ++optionsRequestID
  optionsController?.abort()
  optionsController = new AbortController()
  loadError.value = false
  if (append) loadingMore.value = true
  else {
    loading.value = true
    options.value = []
    activeIndex.value = -1
  }

  const search = query.value
  try {
    const response = await fetchRecords(
      props.backendBaseURL,
      props.schemaName,
      props.resource.name,
      { ...defaultRecordsQuery(), page: nextPage, limit: 20, search },
      optionsController.signal,
    )
    if (requestID !== optionsRequestID) return
    const incoming = response.records.map((record) => relationOption(record, props.resource, props.referencedColumns))
    const combined = append ? [...options.value, ...incoming] : incoming
    options.value = [...new Map(combined.map((option) => [option.key, option])).values()]
    page.value = response.page
    hasNext.value = response.has_next
    loadedSearch.value = search
  } catch (error) {
    if (requestID !== optionsRequestID || error instanceof DOMException && error.name === 'AbortError') return
    loadError.value = true
    hasNext.value = false
  } finally {
    if (requestID === optionsRequestID) {
      loading.value = false
      loadingMore.value = false
    }
  }
}

async function loadSelected() {
  const requestID = ++selectionRequestID
  selectionController?.abort()
  selectionLoadError.value = false
  if (!completeSelection.value) {
    selectedRecord.value = null
    return
  }
  if (selectedRecord.value && relationRecordKey(selectedRecord.value, props.referencedColumns) === selectedKey.value) return

  selectedRecord.value = null
  selectionController = new AbortController()
  try {
    const response = await fetchRecords(
      props.backendBaseURL,
      props.schemaName,
      props.resource.name,
      {
        ...defaultRecordsQuery(),
        limit: 1,
        filters: exactRelationFilters(props.referencedColumns, props.selectedValues),
      },
      selectionController.signal,
    )
    if (requestID !== selectionRequestID) return
    const record = response.records[0]
    if (record && relationRecordKey(record, props.referencedColumns) === selectedKey.value) selectedRecord.value = record
  } catch (error) {
    if (requestID !== selectionRequestID || error instanceof DOMException && error.name === 'AbortError') return
    selectionLoadError.value = true
  }
}

function selectOption(option: RelationOption) {
  if (props.disabled) return
  selectedRecord.value = option.record
  selectionLoadError.value = false
  emit('select', option.record)
  closeDropdown()
  restoreFocus()
}

function clearSelection() {
  if (props.disabled || !props.nullable) return
  selectedRecord.value = null
  selectionLoadError.value = false
  emit('clear')
  closeDropdown()
  if (loadedSearch.value) void loadOptions(1, false)
  restoreFocus()
}

let restoringFocus = false

function handleFocus() {
  if (!restoringFocus) openDropdown()
}

function restoreFocus() {
  void nextTick(() => {
    restoringFocus = true
    input.value?.focus()
    restoringFocus = false
  })
}

function handleKeydown(event: KeyboardEvent) {
  if (props.disabled) return
  if (event.key === 'Escape' && isOpen.value) {
    event.preventDefault()
    event.stopPropagation()
    closeDropdown()
    restoreFocus()
    return
  }
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    if (!isOpen.value) openDropdown()
    const direction = event.key === 'ArrowDown' ? 1 : -1
    const count = visibleOptions.value.length
    if (count > 0) activeIndex.value = (activeIndex.value + direction + count) % count
    void nextTick(scrollActiveOption)
    return
  }
  if (event.key === 'Enter' && isOpen.value) {
    event.preventDefault()
    const option = visibleOptions.value[activeIndex.value]
    if (option) selectOption(option)
    return
  }
  if (event.key === 'Tab') closeDropdown()
}

function scrollActiveOption() {
  const id = activeDescendant.value
  if (id) document.getElementById(id)?.scrollIntoView?.({ block: 'nearest' })
}

function updatePosition() {
  void nextTick(() => {
    if (!isOpen.value || !root.value) return
    const rect = root.value.getBoundingClientRect()
    const viewportWidth = window.innerWidth || 1024
    const viewportHeight = window.innerHeight || 768
    const width = Math.min(Math.max(rect.width, 240), viewportWidth - 16)
    const left = Math.max(8, Math.min(rect.left, viewportWidth - width - 8))
    const below = viewportHeight - rect.bottom - 12
    const above = rect.top - 12
    const useAbove = below < 160 && above > below
    dropdownStyle.value = {
      left: `${left}px`,
      width: `${width}px`,
      maxHeight: `${Math.max(120, Math.min(320, useAbove ? above : below))}px`,
      ...(useAbove
        ? { bottom: `${viewportHeight - rect.top + 4}px`, top: 'auto' }
        : { top: `${rect.bottom + 4}px`, bottom: 'auto' }),
    }
  })
}

function handleOutsidePointer(event: PointerEvent) {
  const target = event.target
  if (!(target instanceof Node)) return
  if (!root.value?.contains(target) && !dropdown.value?.contains(target)) closeDropdown()
}

watch([selectedKey, () => props.hasSelection], () => void loadSelected())
onMounted(() => {
  document.addEventListener('pointerdown', handleOutsidePointer)
  window.addEventListener('resize', updatePosition)
  window.addEventListener('scroll', updatePosition, true)
  void loadOptions(1, false)
  void loadSelected()
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', handleOutsidePointer)
  window.removeEventListener('resize', updatePosition)
  window.removeEventListener('scroll', updatePosition, true)
  window.clearTimeout(searchTimer)
  optionsController?.abort()
  selectionController?.abort()
})
</script>

<template>
  <div ref="root" class="relation-selector">
    <div class="relation-input-wrap">
      <input
        :id="inputId"
        ref="input"
        type="text"
        role="combobox"
        :value="isOpen ? query : selectedText"
        :placeholder="t('relation.select')"
        :aria-label="t('relation.comboboxLabel', { label })"
        :aria-expanded="isOpen"
        :aria-controls="listboxID"
        :aria-activedescendant="activeDescendant"
        :aria-required="required"
        :aria-busy="loading"
        aria-autocomplete="list"
        autocomplete="off"
        :disabled="disabled"
        @focus="handleFocus"
        @click="openDropdown"
        @input="handleInput"
        @keydown="handleKeydown"
      />
      <button
        v-if="nullable && hasSelection && !disabled"
        type="button"
        class="relation-clear"
        :aria-label="t('relation.clear')"
        @mousedown.prevent
        @click="clearSelection"
      >×</button>
    </div>

    <Teleport to="body">
      <div
        v-if="isOpen && !disabled"
        :id="`${listboxID}-dropdown`"
        ref="dropdown"
        class="relation-dropdown"
        :style="dropdownStyle"
      >
        <p v-if="loading" class="relation-state" role="status">{{ t('relation.loading') }}</p>
        <div v-else-if="loadError" class="relation-state relation-error" role="alert">
          <span>{{ t('relation.loadError') }}</span>
          <button type="button" class="secondary-button" @mousedown.prevent @click="loadOptions(1, false)">{{ t('common.retry') }}</button>
        </div>
        <p v-else-if="visibleOptions.length === 0" class="relation-state" role="status">{{ t('relation.noResults') }}</p>
        <div v-else :id="listboxID" role="listbox" :aria-label="t('relation.optionsLabel', { label })">
          <button
            v-for="(option, index) in visibleOptions"
            :id="`${listboxID}-option-${index}`"
            :key="option.key"
            type="button"
            role="option"
            class="relation-option"
            :class="{ active: activeIndex === index, selected: option.key === selectedKey }"
            :aria-selected="option.key === selectedKey"
            @mousedown.prevent
            @mouseenter="activeIndex = index"
            @click="selectOption(option)"
          >
            <span>{{ option.label }}</span>
            <code v-if="option.label !== option.keyLabel">{{ option.keyLabel }}</code>
          </button>
        </div>
        <p v-if="selectionLoadError && hasSelection" class="relation-selection-error" role="alert">
          {{ t('relation.selectionLoadError') }}
        </p>
        <button
          v-if="hasNext && !loading && !loadError"
          type="button"
          class="relation-load-more secondary-button"
          :disabled="loadingMore"
          @mousedown.prevent
          @click="loadOptions(page + 1, true)"
        >{{ loadingMore ? t('relation.loading') : t('relation.loadMore') }}</button>
      </div>
    </Teleport>
  </div>
</template>
