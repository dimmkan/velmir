<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { authState, can, initializeAuth, login, logout } from './auth'
import LoginPage from './LoginPage.vue'
import AdministrationPage from './AdministrationPage.vue'
import RecordsTable from './RecordsTable.vue'
import AppFooter from './AppFooter.vue'
import { fetchReadiness, type DatabaseState, type ReadinessResponse } from './readiness'
import {
  fetchSchema,
  SchemaRequestError,
  type ResourceMetadata,
  type SchemaResponse,
} from './schema'

type ViewState = 'loading' | 'ready' | 'error'
type SchemaViewState = 'loading' | 'ready' | 'empty' | 'not_configured' | 'forbidden' | 'error'
type RouteState = 'loading' | 'login' | 'application' | 'error'

const { t } = useI18n()
const backendBaseURL = (import.meta.env.VITE_BACKEND_BASE_URL ?? '').replace(/\/+$/, '')
const routeState = ref<RouteState>('loading')
const applicationSection = ref<'resources' | 'administration'>('resources')
const viewState = ref<ViewState>('loading')
const readiness = ref<ReadinessResponse | null>(null)
const schemaViewState = ref<SchemaViewState>('loading')
const catalog = ref<SchemaResponse | null>(null)
const selectedResourceKey = ref('')
const formDirty = ref(false)
let intendedRoute = window.location.pathname === '/login'
  ? '/'
  : `${window.location.pathname}${window.location.search}${window.location.hash}`
let initialized = false

const selectedResource = computed<ResourceMetadata | null>(() => {
  for (const schema of catalog.value?.schemas ?? []) {
    for (const resource of schema.resources) {
      if (resourceKey(schema.name, resource.name) === selectedResourceKey.value) return resource
    }
  }
  return null
})

const selectedSchemaName = computed(() => {
  for (const schema of catalog.value?.schemas ?? []) {
    if (schema.resources.some((resource) => resourceKey(schema.name, resource.name) === selectedResourceKey.value)) {
      return schema.name
    }
  }
  return ''
})

const readAllowed = computed(() => Boolean(selectedResource.value) &&
  can(selectedSchemaName.value, selectedResource.value!.name, 'read'))
const createAllowed = computed(() => Boolean(selectedResource.value) &&
  can(selectedSchemaName.value, selectedResource.value!.name, 'create'))
const updateAllowed = computed(() => Boolean(selectedResource.value) &&
  can(selectedSchemaName.value, selectedResource.value!.name, 'update'))
const deleteAllowed = computed(() => Boolean(selectedResource.value) &&
  can(selectedSchemaName.value, selectedResource.value!.name, 'delete'))

function resourceKey(schema: string, resource: string) {
  return JSON.stringify([schema, resource])
}

function databaseStateLabel(state: DatabaseState | undefined) {
  if (viewState.value === 'loading') return t('readiness.checking')
  const keys: Record<DatabaseState, string> = {
    ready: 'readiness.ready',
    unavailable: 'readiness.unavailable',
    not_configured: 'readiness.notConfigured',
  }
  return state ? t(keys[state]) : t('common.unknown')
}

async function loadReadiness() {
  viewState.value = 'loading'
  readiness.value = null

  const controller = new AbortController()
  const timeout = window.setTimeout(() => controller.abort(), 5000)

  try {
    readiness.value = await fetchReadiness(backendBaseURL, controller.signal)
    viewState.value = 'ready'
  } catch {
    viewState.value = 'error'
  } finally {
    window.clearTimeout(timeout)
  }
}

async function loadSchema() {
  schemaViewState.value = 'loading'
  catalog.value = null
  selectedResourceKey.value = ''

  const controller = new AbortController()
  const timeout = window.setTimeout(() => controller.abort(), 10000)

  try {
    const response = await fetchSchema(backendBaseURL, controller.signal)
    catalog.value = response
    const firstSchema = response.schemas.find((schema) => schema.resources.length > 0)
    if (!firstSchema) {
      schemaViewState.value = 'empty'
      return
    }
    if (!restoreResourceFromURL()) {
      selectResource(firstSchema.name, firstSchema.resources[0].name, false)
    }
    schemaViewState.value = 'ready'
  } catch (error) {
    if (error instanceof SchemaRequestError && error.status === 403) {
      schemaViewState.value = 'forbidden'
    } else {
      schemaViewState.value =
        error instanceof SchemaRequestError && error.code === 'TARGET_NOT_CONFIGURED'
          ? 'not_configured'
          : 'error'
    }
  } finally {
    window.clearTimeout(timeout)
  }
}

function restoreResourceFromURL() {
  const url = new URL(window.location.href)
  const schema = url.searchParams.get('schema')
  const resource = url.searchParams.get('resource')
  if (!schema || !resource) return false
  const exists = catalog.value?.schemas.some((item) =>
    item.name === schema && item.resources.some((itemResource) => itemResource.name === resource),
  )
  if (!exists) return false
  selectedResourceKey.value = resourceKey(schema, resource)
  return true
}

function selectResource(schema: string, resource: string, push = true) {
  if (push && formDirty.value && selectedResourceKey.value !== resourceKey(schema, resource) && !window.confirm(t('records.confirmDiscard'))) return
  formDirty.value = false
  applicationSection.value = 'resources'
  const url = new URL(window.location.href)
  url.pathname = '/'
  url.searchParams.set('schema', schema)
  url.searchParams.set('resource', resource)
  if (push) {
    for (const key of ['page', 'limit', 'sort', 'direction', 'search', 'search_column', 'filter']) {
      url.searchParams.delete(key)
    }
  }
  window.history[push ? 'pushState' : 'replaceState']({}, '', `${url.pathname}${url.search}${url.hash}`)
  selectedResourceKey.value = resourceKey(schema, resource)
}

function showAdministration() {
  if (!authState.can_admin) return
  if (formDirty.value && !window.confirm(t('records.confirmDiscard'))) return
  formDirty.value = false
  applicationSection.value = 'administration'
  window.history.pushState({}, '', '/administration')
}

function showLogin() {
  if (window.location.pathname !== '/login') {
    intendedRoute = `${window.location.pathname}${window.location.search}${window.location.hash}`
    window.history.replaceState({}, '', '/login')
  }
  routeState.value = 'login'
}

function showApplication() {
  if (window.location.pathname === '/login') window.history.replaceState({}, '', intendedRoute || '/')
  applicationSection.value = window.location.pathname === '/administration' && authState.can_admin
    ? 'administration'
    : 'resources'
  if (window.location.pathname === '/administration' && !authState.can_admin) window.history.replaceState({}, '', '/')
  routeState.value = 'application'
  void loadReadiness()
  void loadSchema()
}

async function initialize() {
  routeState.value = 'loading'
  await initializeAuth()
  initialized = true
  if (authState.status === 'authenticated') showApplication()
  else if (authState.status === 'anonymous') showLogin()
  else routeState.value = 'error'
}

async function submitLogin(email: string, password: string) {
  if (await login(email, password)) showApplication()
}

async function submitLogout() {
  if (await logout()) showLogin()
}

function handlePopState() {
  if (authState.status !== 'authenticated') {
    showLogin()
    return
  }
  if (window.location.pathname === '/login') {
    showApplication()
    return
  }
  routeState.value = 'application'
  applicationSection.value = window.location.pathname === '/administration' && authState.can_admin
    ? 'administration'
    : 'resources'
  if (applicationSection.value === 'resources') restoreResourceFromURL()
}

watch(() => authState.status, (status) => {
  if (initialized && status === 'anonymous' && routeState.value !== 'login') showLogin()
})

onMounted(() => {
  window.addEventListener('popstate', handlePopState)
  void initialize()
})
onBeforeUnmount(() => window.removeEventListener('popstate', handlePopState))
</script>

<template>
  <div class="route-shell">
    <LoginPage v-if="routeState === 'login'" :pending="authState.loginPending" :error="authState.loginError" @submit="submitLogin" />
    <main v-else-if="routeState === 'loading'" class="auth-state" role="status">
      <p class="eyebrow">Velmir</p>
      <h1>{{ t('auth.checkingSession') }}</h1>
    </main>
    <main v-else-if="routeState === 'error'" class="auth-state" role="alert">
      <p class="eyebrow">{{ t('auth.connectionError') }}</p>
      <h1>{{ t('auth.unavailable') }}</h1>
      <p>{{ t(authState.error) }}</p>
      <button type="button" class="primary-button" @click="initialize">{{ t('common.retry') }}</button>
    </main>
    <div v-else class="app-layout">
      <aside class="sidebar">
        <div class="brand">
          <img class="brand-logo" src="/brand/velmir-logo-compact-dark.svg" alt="Velmir" />
          <img class="brand-mark" src="/brand/velmir-mark-dark.svg" alt="Velmir" />
        </div>

        <div class="sidebar-content">
          <nav v-if="authState.can_admin" :aria-label="t('navigation.administration')" class="administration-nav">
            <button
              type="button"
              class="resource-link"
              :class="{ active: applicationSection === 'administration' }"
              @click="showAdministration"
            >{{ t('navigation.administration') }}</button>
          </nav>
          <p v-if="schemaViewState === 'loading'" class="sidebar-message">{{ t('records.loadingSchema') }}</p>
          <div v-else-if="schemaViewState === 'not_configured'" class="sidebar-message">
            {{ t('records.targetNotConfigured') }}
          </div>
          <div v-else-if="schemaViewState === 'empty'" class="sidebar-message">
            {{ t('records.noTables') }}
          </div>
          <div v-else-if="schemaViewState === 'forbidden'" class="sidebar-message sidebar-error" role="alert">
            {{ t('records.noReadPermission') }}
          </div>
          <div v-else-if="schemaViewState === 'error'" class="sidebar-message sidebar-error" role="alert">
            <p>{{ t('records.schemaLoadFailed') }}</p>
            <button type="button" class="secondary-button" @click="loadSchema">{{ t('common.retry') }}</button>
          </div>
          <nav v-else :aria-label="t('navigation.resources')">
            <section v-for="schema in catalog?.schemas" :key="schema.name" class="schema-group">
              <h2>{{ schema.name }}</h2>
              <button
                v-for="resource in schema.resources"
                :key="resource.name"
                type="button"
                class="resource-link"
                :class="{ active: selectedResourceKey === resourceKey(schema.name, resource.name) }"
                @click="selectResource(schema.name, resource.name)"
              >
                <span>{{ resource.name }}</span>
                <span class="resource-kind">{{ resource.kind }}</span>
              </button>
            </section>
          </nav>
        </div>

        <section class="account" :aria-label="t('auth.currentUser')">
          <strong>{{ authState.user?.display_name }}</strong>
          <span>{{ authState.user?.email }}</span>
          <p v-if="authState.logoutError" class="account-error" role="alert">{{ t(authState.logoutError) }}</p>
          <button type="button" class="logout-button" :disabled="authState.logoutPending" @click="submitLogout">
            {{ authState.logoutPending ? t('auth.signingOut') : t('auth.signOut') }}
          </button>
        </section>

        <section class="readiness" :aria-label="t('readiness.label')">
          <div>
            <span>{{ t('readiness.backend') }}</span>
            <strong :class="`status-${viewState === 'error' ? 'unavailable' : viewState}`">
              {{ viewState === 'loading' ? t('readiness.checking') : viewState === 'error' ? t('readiness.unavailable') : t('readiness.available') }}
            </strong>
          </div>
          <div>
            <span>{{ t('readiness.adminDatabase') }}</span>
            <strong :class="`status-${readiness?.admin_database ?? viewState}`">
              {{ databaseStateLabel(readiness?.admin_database) }}
            </strong>
          </div>
          <div>
            <span>{{ t('readiness.targetDatabase') }}</span>
            <strong :class="`status-${readiness?.target_database ?? viewState}`">
              {{ databaseStateLabel(readiness?.target_database) }}
            </strong>
          </div>
          <button v-if="viewState === 'error'" type="button" class="readiness-retry" @click="loadReadiness">
            {{ t('readiness.retry') }}
          </button>
        </section>
      </aside>

      <main class="content">
        <AdministrationPage v-if="applicationSection === 'administration' && authState.can_admin" :backend-base-u-r-l="backendBaseURL" />
        <template v-else-if="selectedResource">
          <header class="resource-header">
            <p class="eyebrow">{{ selectedSchemaName }}</p>
            <div class="resource-title">
              <h2>{{ selectedResource.name }}</h2>
              <span class="kind-badge">{{ selectedResource.kind }}</span>
            </div>
            <p>{{ t('records.metadataDescription', { mode: catalog?.write_enabled ? t('records.readAndWrite') : t('records.readOnly') }) }}</p>
          </header>
          <RecordsTable
            :backend-base-u-r-l="backendBaseURL"
            :schema-name="selectedSchemaName"
            :resource="selectedResource"
            :write-enabled="catalog?.write_enabled ?? false"
            :read-allowed="readAllowed"
            :create-allowed="createAllowed"
            :update-allowed="updateAllowed"
            :delete-allowed="deleteAllowed"
            @dirty-change="formDirty = $event"
          />
        </template>

        <section v-else class="empty-state" :role="schemaViewState === 'forbidden' ? 'alert' : undefined">
          <p class="eyebrow">{{ t('records.databaseRecords') }}</p>
          <h2 v-if="schemaViewState === 'loading'">{{ t('records.loadingMetadata') }}</h2>
          <h2 v-else-if="schemaViewState === 'not_configured'">{{ t('records.targetNotConfigured') }}</h2>
          <h2 v-else-if="schemaViewState === 'empty'">{{ t('records.noResources') }}</h2>
          <h2 v-else-if="schemaViewState === 'forbidden'">{{ t('errors.forbidden') }}</h2>
          <h2 v-else-if="schemaViewState === 'error'">{{ t('records.schemaUnavailable') }}</h2>
          <h2 v-else>{{ t('records.selectResource') }}</h2>
          <p v-if="schemaViewState === 'not_configured'">{{ t('records.targetNotConfiguredHint') }}</p>
          <p v-else-if="schemaViewState === 'empty'">{{ t('records.noResourcesHint') }}</p>
          <p v-else-if="schemaViewState === 'forbidden'">{{ t('records.forbiddenHint') }}</p>
          <p v-else-if="schemaViewState === 'error'">{{ t('records.schemaErrorHint') }}</p>
          <p v-else-if="schemaViewState === 'ready'">{{ t('records.selectResourceHint') }}</p>
        </section>
      </main>
    </div>
    <AppFooter />
  </div>
</template>
