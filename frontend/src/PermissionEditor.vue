<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { FieldPermission, FieldPermissionAction, ResourcePermission } from './auth'
import { fieldPermission, permissionOperations } from './admin'
import type { ResourceMetadata, SchemaMetadata } from './schema'

const props = withDefaults(defineProps<{
  permissions: ResourcePermission[]
  schemas?: SchemaMetadata[]
  disabled?: boolean
}>(), { schemas: () => [] })
const emit = defineEmits<{
  change: [permissions: ResourcePermission[]]
}>()
const { t } = useI18n()
const fieldActions: FieldPermissionAction[] = ['create', 'update']

function normalizePermission(permission: ResourcePermission): ResourcePermission {
  if (permission.schema === '*' && permission.resource === '*') {
    return { ...permission, field_permissions: [] }
  }
  const configured = permission.field_permissions ?? []
  const fields: FieldPermission[] = []
  for (const action of fieldActions) {
    if (!permission.operations.includes(action)) continue
    const current = configured.find((item) => item.action === action)
    fields.push(current ? { ...current, columns: [...current.columns] } : { action, mode: 'unrestricted', columns: [] })
  }
  return { ...permission, field_permissions: fields }
}

function update(index: number, patch: Partial<ResourcePermission>) {
  emit('change', props.permissions.map((permission, itemIndex) =>
    itemIndex === index ? normalizePermission({ ...permission, ...patch }) : permission,
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

function setFieldMode(index: number, action: FieldPermissionAction, mode: FieldPermission['mode']) {
  const permission = props.permissions[index]
  const current = fieldPermission(permission, action)
  update(index, {
    field_permissions: [
      ...(permission.field_permissions ?? []).filter((item) => item.action !== action),
      { action, mode, columns: mode === 'unrestricted' ? [] : [...current.columns] },
    ],
  })
}

function toggleField(index: number, action: FieldPermissionAction, column: string) {
  const permission = props.permissions[index]
  const current = fieldPermission(permission, action)
  if (current.mode !== 'allowlist') return
  const selected = new Set(current.columns)
  if (selected.has(column)) selected.delete(column)
  else selected.add(column)
  update(index, {
    field_permissions: [
      ...(permission.field_permissions ?? []).filter((item) => item.action !== action),
      {
        action,
        mode: 'allowlist',
        columns: writableColumns(permission, action).map((item) => item.name).filter((name) => selected.has(name)),
      },
    ],
  })
}

function resourceMetadata(permission: ResourcePermission): ResourceMetadata | undefined {
  for (const schema of props.schemas) {
    if (schema.name !== permission.schema) continue
    return schema.resources.find((resource) => resource.name === permission.resource)
  }
  return undefined
}

function writableColumns(permission: ResourcePermission, _action: FieldPermissionAction) {
  return resourceMetadata(permission)?.columns.filter((column) =>
    column.editable && !column.generated && column.identity === '' && !column.primary_key,
  ) ?? []
}

function addPermission() {
  emit('change', [...props.permissions, { schema: '', resource: '', operations: ['read'], field_permissions: [] }])
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
          {{ t(`permissions.operation.${operation}`) }}
        </label>
      </fieldset>

      <template v-if="permission.schema !== '*' || permission.resource !== '*'">
        <fieldset
          v-for="action in fieldActions.filter((item) => permission.operations.includes(item))"
          :key="action"
          class="field-permission-editor"
        >
          <legend>{{ t(`permissions.fieldsFor.${action}`) }}</legend>
          <label>
            <input
              type="radio"
              :name="`field-mode-${index}-${action}`"
              :checked="fieldPermission(permission, action).mode === 'unrestricted'"
              :disabled="disabled"
              @change="setFieldMode(index, action, 'unrestricted')"
            />
            {{ t('permissions.allFields') }}
          </label>
          <label>
            <input
              type="radio"
              :name="`field-mode-${index}-${action}`"
              :checked="fieldPermission(permission, action).mode === 'allowlist'"
              :disabled="disabled"
              @change="setFieldMode(index, action, 'allowlist')"
            />
            {{ t('permissions.selectedFields') }}
          </label>

          <template v-if="fieldPermission(permission, action).mode === 'allowlist'">
            <p v-if="!resourceMetadata(permission)" class="field-error">{{ t('permissions.resourceMetadataUnavailable') }}</p>
            <div v-else class="field-permission-columns">
              <label v-for="column in writableColumns(permission, action)" :key="column.name">
                <input
                  type="checkbox"
                  :checked="fieldPermission(permission, action).columns.includes(column.name)"
                  :disabled="disabled"
                  @change="toggleField(index, action, column.name)"
                />
                {{ column.name }}
              </label>
            </div>
            <p v-if="resourceMetadata(permission) && writableColumns(permission, action).length === 0" class="field-error">
              {{ t('permissions.noSelectableFields') }}
            </p>
            <p
              v-if="fieldPermission(permission, action).columns.length === 0"
              class="empty-allowlist-warning"
              role="alert"
            >{{ t('permissions.emptyAllowlistWarning') }}</p>
          </template>
        </fieldset>
      </template>
      <p v-else class="wildcard-field-note">{{ t('permissions.wildcardUnrestricted') }}</p>

      <button type="button" class="danger-button" :disabled="disabled" @click="removePermission(index)">{{ t('permissions.remove') }}</button>
    </div>
    <button type="button" class="secondary-button light" :disabled="disabled" @click="addPermission">{{ t('permissions.add') }}</button>
  </div>
</template>

<style scoped>
.field-permission-editor {
  display: grid;
  gap: 0.55rem;
  flex-basis: 100%;
  padding: 0.8rem;
}

.field-permission-columns {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(10rem, 1fr));
  gap: 0.45rem;
  max-height: 14rem;
  overflow: auto;
}

.empty-allowlist-warning {
  margin: 0;
  padding: 0.65rem;
  color: #8a4b00;
  background: #fff4d6;
  border: 1px solid #e6bd55;
  font-weight: 600;
}

.wildcard-field-note {
  flex-basis: 100%;
  margin: 0;
}
</style>
