<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { authState, getCSRFToken } from './auth'
import { errorTranslationKey, fieldErrorTranslationKey } from './i18n'
import {
  AdminRequestError,
  createAdminClient,
  validateDisplayName,
  validatePassword,
  validateRoleForm,
  validateUserForm,
  type AdminRole,
  type AdminUser,
  type FormErrors,
  type RoleForm,
  type UserForm,
} from './admin'
import PermissionEditor from './PermissionEditor.vue'

const props = defineProps<{ backendBaseURL: string }>()
const { t } = useI18n()
const client = createAdminClient(props.backendBaseURL, getCSRFToken)
const tab = ref<'users' | 'roles'>('users')
const users = ref<AdminUser[]>([])
const roles = ref<AdminRole[]>([])
const loading = ref(true)
const loadError = ref('')
const operation = ref('')
const actionError = ref('')
const notice = ref('')
const createErrors = reactive<FormErrors>({})
const roleErrors = reactive<FormErrors>({})
const displayDrafts = reactive<Record<number, string>>({})
const passwordDrafts = reactive<Record<number, string>>({})
const createForm = reactive<UserForm>({ email: '', display_name: '', password: '' })
const newRole = reactive<RoleForm>({ name: '', description: '', permissions: [] })
const selectedRoleID = ref<number | null>(null)
const roleDraft = reactive<RoleForm>({ name: '', description: '', permissions: [] })

const selectedRole = computed(() => roles.value.find((role) => role.id === selectedRoleID.value) ?? null)

function clearErrors(errors: FormErrors) {
  for (const key of Object.keys(errors)) delete errors[key]
}

function applyErrors(errors: FormErrors, target: FormErrors) {
  clearErrors(target)
  Object.assign(target, errors)
}

function localizedFieldErrors(error: AdminRequestError): FormErrors {
  return Object.fromEntries(Object.keys(error.fieldErrors).map((field) => [field, fieldErrorTranslationKey(error.code)]))
}

function showError(error: unknown, fields?: FormErrors) {
  if (error instanceof AdminRequestError) {
    actionError.value = errorTranslationKey(error.code, error.status)
    if (fields) Object.assign(fields, localizedFieldErrors(error))
  } else {
    actionError.value = 'administration.contactBackend'
  }
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const [userResponse, roleResponse] = await Promise.all([client.listUsers(), client.listRoles()])
    users.value = userResponse.users
    roles.value = roleResponse.roles
    for (const user of users.value) {
      displayDrafts[user.id] = user.display_name
      passwordDrafts[user.id] = ''
    }
    if (selectedRoleID.value) selectRole(selectedRoleID.value)
  } catch (error) {
    loadError.value = error instanceof AdminRequestError
      ? errorTranslationKey(error.code, error.status)
      : 'administration.loadFailed'
  } finally {
    loading.value = false
  }
}

async function perform(key: string, action: () => Promise<void>, successKey: string) {
  if (operation.value) return false
  operation.value = key
  actionError.value = ''
  notice.value = ''
  try {
    await action()
    await load()
    notice.value = successKey
    return true
  } catch (error) {
    showError(error)
    return false
  } finally {
    operation.value = ''
  }
}

async function submitUser() {
  applyErrors(validateUserForm(createForm), createErrors)
  actionError.value = ''
  if (Object.keys(createErrors).length > 0) return
  operation.value = 'create-user'
  try {
    await client.createUser({ ...createForm })
    createForm.email = ''
    createForm.display_name = ''
    createForm.password = ''
    await load()
    notice.value = 'administration.userCreated'
  } catch (error) {
    showError(error, createErrors)
  } finally {
    operation.value = ''
  }
}

async function saveDisplayName(user: AdminUser) {
  const errors = validateDisplayName(displayDrafts[user.id] ?? '')
  if (Object.keys(errors).length > 0) {
    actionError.value = errors.display_name
    return
  }
  await perform(`display-${user.id}`, () => client.updateDisplayName(user.id, displayDrafts[user.id]), 'administration.displayNameUpdated')
}

async function toggleUser(user: AdminUser) {
  await perform(
    `status-${user.id}`,
    () => client.setUserActive(user.id, !user.is_active),
    user.is_active ? 'administration.userDisabled' : 'administration.userEnabled',
  )
}

async function resetPassword(user: AdminUser) {
  const password = passwordDrafts[user.id] ?? ''
  const errors = validatePassword(password)
  if (Object.keys(errors).length > 0) {
    actionError.value = errors.password
    return
  }
  if (await perform(`password-${user.id}`, () => client.resetPassword(user.id, password), 'administration.passwordReset')) {
    passwordDrafts[user.id] = ''
  }
}

async function changeRole(user: AdminUser, role: AdminRole, checked: boolean) {
  await perform(`role-${user.id}-${role.id}`, async () => {
    if (checked) await client.assignRole(user.id, role.id)
    else await client.removeRole(user.id, role.id)
  }, checked ? 'administration.roleAssigned' : 'administration.roleRemoved')
}

function selectRole(roleID: number) {
  const role = roles.value.find((item) => item.id === roleID)
  if (!role || role.is_system) {
    selectedRoleID.value = null
    return
  }
  selectedRoleID.value = role.id
  roleDraft.name = role.name
  roleDraft.description = role.description
  roleDraft.permissions = role.permissions.map((permission) => ({ ...permission, operations: [...permission.operations] }))
  clearErrors(roleErrors)
  actionError.value = ''
}

async function submitRole() {
  applyErrors(validateRoleForm(newRole), roleErrors)
  actionError.value = ''
  if (Object.keys(roleErrors).length > 0) return
  operation.value = 'create-role'
  try {
    const response = await client.createRole({ ...newRole, permissions: newRole.permissions.map((item) => ({ ...item, operations: [...item.operations] })) })
    newRole.name = ''
    newRole.description = ''
    newRole.permissions = []
    await load()
    selectRole(response.role.id)
    notice.value = 'administration.roleCreated'
  } catch (error) {
    showError(error, roleErrors)
  } finally {
    operation.value = ''
  }
}

async function saveRole() {
  if (!selectedRole.value) return
  applyErrors(validateRoleForm(roleDraft), roleErrors)
  actionError.value = ''
  if (Object.keys(roleErrors).length > 0) return
  operation.value = `save-role-${selectedRole.value.id}`
  try {
    await client.updateRole(selectedRole.value.id, roleDraft.name, roleDraft.description)
    await client.replacePermissions(selectedRole.value.id, roleDraft.permissions)
    await load()
    notice.value = 'administration.roleUpdated'
  } catch (error) {
    showError(error, roleErrors)
  } finally {
    operation.value = ''
  }
}

onMounted(load)
</script>

<template>
  <section class="administration" aria-labelledby="administration-title">
    <header class="resource-header">
      <p class="eyebrow">{{ t('administration.eyebrow') }}</p>
      <h2 id="administration-title">{{ t('navigation.administration') }}</h2>
      <p>{{ t('administration.description') }}</p>
    </header>

    <div class="admin-tabs" role="tablist" :aria-label="t('administration.sections')">
      <button type="button" role="tab" :aria-selected="tab === 'users'" :class="{ active: tab === 'users' }" @click="tab = 'users'">{{ t('administration.users') }}</button>
      <button type="button" role="tab" :aria-selected="tab === 'roles'" :class="{ active: tab === 'roles' }" @click="tab = 'roles'">{{ t('administration.roles') }}</button>
    </div>

    <div v-if="loading" class="records-state" role="status">{{ t('administration.loading') }}</div>
    <div v-else-if="loadError" class="records-state records-error" role="alert">
      {{ t(loadError) }}
      <button type="button" class="secondary-button" @click="load">{{ t('common.retry') }}</button>
    </div>
    <template v-else>
      <p v-if="notice" class="admin-notice" role="status">{{ t(notice) }}</p>
      <p v-if="actionError" class="form-server-error" role="alert">{{ t(actionError) }}</p>

      <div v-if="tab === 'users'" class="admin-stack">
        <form class="admin-panel admin-form" @submit.prevent="submitUser">
          <h3>{{ t('administration.createUser') }}</h3>
          <div class="admin-form-grid">
            <label>{{ t('auth.email') }}<input v-model="createForm.email" type="text" autocomplete="off" :disabled="Boolean(operation)" /></label>
            <label>{{ t('administration.displayName') }}<input v-model="createForm.display_name" :disabled="Boolean(operation)" /></label>
            <label>{{ t('auth.password') }}<input v-model="createForm.password" type="password" autocomplete="new-password" :disabled="Boolean(operation)" /></label>
          </div>
          <p v-for="(error, field) in createErrors" :key="field" class="field-error" role="alert">{{ t(error) }}</p>
          <button type="submit" class="primary-button" :disabled="Boolean(operation)">{{ operation === 'create-user' ? t('administration.creating') : t('administration.createUser') }}</button>
        </form>

        <div class="table-wrapper admin-table-wrapper">
          <table>
            <thead><tr><th>{{ t('administration.user') }}</th><th>{{ t('administration.status') }}</th><th>{{ t('administration.roles') }}</th><th>{{ t('administration.security') }}</th></tr></thead>
            <tbody>
              <tr v-for="user in users" :key="user.id">
                <td>
                  <strong>{{ user.email }}</strong>
                  <div class="inline-field">
                    <input v-model="displayDrafts[user.id]" :aria-label="t('administration.displayName')" :disabled="Boolean(operation)" />
                    <button type="button" class="secondary-button light" :disabled="Boolean(operation)" @click="saveDisplayName(user)">{{ t('administration.saveName') }}</button>
                  </div>
                </td>
                <td>
                  <span :class="user.is_active ? 'active-state' : 'inactive-state'">{{ user.is_active ? t('administration.active') : t('administration.disabled') }}</span>
                  <button
                    type="button"
                    class="secondary-button light status-action"
                    :disabled="Boolean(operation) || (user.id === authState.user?.id && user.is_active)"
                    :title="user.id === authState.user?.id && user.is_active ? t('administration.selfDisableHint') : ''"
                    @click="toggleUser(user)"
                  >{{ user.is_active ? t('administration.disable') : t('administration.enable') }}</button>
                </td>
                <td>
                  <label v-for="role in roles" :key="role.id" class="role-checkbox">
                    <input
                      type="checkbox"
                      :checked="user.roles.includes(role.name)"
                      :disabled="Boolean(operation) || (user.id === authState.user?.id && role.name === 'admin')"
                      @change="changeRole(user, role, ($event.target as HTMLInputElement).checked)"
                    />
                    {{ role.name }}
                  </label>
                </td>
                <td>
                  <label class="password-reset">{{ t('administration.newPassword') }}<input v-model="passwordDrafts[user.id]" type="password" autocomplete="new-password" :disabled="Boolean(operation)" /></label>
                  <button type="button" class="secondary-button light" :disabled="Boolean(operation)" @click="resetPassword(user)">{{ t('administration.resetPassword') }}</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div v-else class="admin-stack">
        <form class="admin-panel admin-form" @submit.prevent="submitRole">
          <h3>{{ t('administration.createRole') }}</h3>
          <div class="admin-form-grid two-columns">
            <label>{{ t('administration.name') }}<input v-model="newRole.name" placeholder="content_editor" :disabled="Boolean(operation)" /></label>
            <label>{{ t('administration.roleDescription') }}<input v-model="newRole.description" :disabled="Boolean(operation)" /></label>
          </div>
          <PermissionEditor :permissions="newRole.permissions" :disabled="Boolean(operation)" @change="newRole.permissions = $event" />
          <p v-for="(error, field) in roleErrors" :key="field" class="field-error" role="alert">{{ t(error) }}</p>
          <button type="submit" class="primary-button" :disabled="Boolean(operation)">{{ operation === 'create-role' ? t('administration.creating') : t('administration.createRoleAction') }}</button>
        </form>

        <div class="admin-panel">
          <h3>{{ t('administration.roles') }}</h3>
          <div class="role-list">
            <article v-for="role in roles" :key="role.id" class="role-card">
              <div>
                <strong>{{ role.name }}</strong>
                <span v-if="role.is_system" class="kind-badge">{{ t('administration.system') }}</span>
                <p>{{ role.description || t('administration.noDescription') }}</p>
                <code>{{ t('administration.permissionEntries', { count: role.permissions.length }) }}</code>
              </div>
              <button v-if="!role.is_system" type="button" class="secondary-button light" @click="selectRole(role.id)">{{ t('common.edit') }}</button>
            </article>
          </div>
        </div>

        <form v-if="selectedRole" class="admin-panel admin-form" @submit.prevent="saveRole">
          <h3>{{ t('administration.editRole', { name: selectedRole.name }) }}</h3>
          <div class="admin-form-grid two-columns">
            <label>{{ t('administration.name') }}<input v-model="roleDraft.name" :disabled="Boolean(operation)" /></label>
            <label>{{ t('administration.roleDescription') }}<input v-model="roleDraft.description" :disabled="Boolean(operation)" /></label>
          </div>
          <PermissionEditor :permissions="roleDraft.permissions" :disabled="Boolean(operation)" @change="roleDraft.permissions = $event" />
          <p v-for="(error, field) in roleErrors" :key="field" class="field-error" role="alert">{{ t(error) }}</p>
          <button type="submit" class="primary-button" :disabled="Boolean(operation)">{{ operation ? t('administration.saving') : t('administration.saveRole') }}</button>
        </form>
      </div>
    </template>
  </section>
</template>
