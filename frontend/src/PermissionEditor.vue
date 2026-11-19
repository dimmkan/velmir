<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { ResourcePermission } from './auth'
import { permissionOperations } from './admin'

const props = defineProps<{
  permissions: ResourcePermission[]
  disabled?: boolean
}>()
const emit = defineEmits<{
  change: [permissions: ResourcePermission[]]
}>()
const { t } = useI18n()

function update(index: number, patch: Partial<ResourcePermission>) {
  emit('change', props.permissions.map((permission, itemIndex) =>
    itemIndex === index ? { ...permission, ...patch } : permission,
  ))
}

function toggleOperation(index: number, operation: ResourcePermission['operations'][number]) {
  const current = props.permissions[index].operations
  update(index, {
    operations: current.includes(operation)
      ? current.filter((item) => item !== operation)
      : permissionOperations.filter((item) => current.includes(item) || item === operation),
  })
}

function addPermission() {
  emit('change', [...props.permissions, { schema: '', resource: '', operations: ['read'] }])
}

function removePermission(index: number) {
  emit('change', props.permissions.filter((_, itemIndex) => itemIndex !== index))
}
</script>

<template>
  <div class="permission-editor">
    <div v-for="(permission, index) in permissions" :key="index" class="permission-row">
      <label>
        <span>{{ t('permissions.schema') }}</span>
        <input
          :value="permission.schema"
          :disabled="disabled"
          :placeholder="t('permissions.schemaPlaceholder')"
          @input="update(index, { schema: ($event.target as HTMLInputElement).value })"
        />
      </label>
      <label>
        <span>{{ t('permissions.resource') }}</span>
        <input
          :value="permission.resource"
          :disabled="disabled"
          :placeholder="t('permissions.resourcePlaceholder')"
          @input="update(index, { resource: ($event.target as HTMLInputElement).value })"
        />
      </label>
      <fieldset>
        <legend>{{ t('permissions.operations') }}</legend>
        <label v-for="operation in permissionOperations" :key="operation">
          <input
            type="checkbox"
            :checked="permission.operations.includes(operation)"
            :disabled="disabled"
            @change="toggleOperation(index, operation)"
          />
          {{ operation }}
        </label>
      </fieldset>
      <button type="button" class="danger-button" :disabled="disabled" @click="removePermission(index)">{{ t('permissions.remove') }}</button>
    </div>
    <button type="button" class="secondary-button light" :disabled="disabled" @click="addPermission">{{ t('permissions.add') }}</button>
  </div>
</template>
