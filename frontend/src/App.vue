<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { authState, can, initializeAuth, login, logout } from './auth'
import LoginPage from './LoginPage.vue'
import RecordsTable from './RecordsTable.vue'
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

const backendBaseURL = (import.meta.env.VITE_BACKEND_BASE_URL ?? '').replace(/\/+$/, '')
const routeState = ref<RouteState>('loading')
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

const databaseStateLabels: Record<DatabaseState, string> = {
  ready: 'Ready',
  unavailable: 'Unavailable',
  not_configured: 'Not configured',
}

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

const createAllowed = computed(() => Boolean(selectedResource.value) &&
  can(selectedSchemaName.value, selectedResource.value!.name, 'create'))
const updateAllowed = computed(() => Boolean(selectedResource.value) &&
  can(selectedSchemaName.value, selectedResource.value!.name, 'update'))

function resourceKey(schema: string, resource: string) {
  return JSON.stringify([schema, resource])
}

function databaseStateLabel(state: DatabaseState | undefined) {
  if (viewState.value === 'loading') return 'Checking'
  return state ? databaseStateLabels[state] : 'Unknown'
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
  if (push && formDirty.value && selectedResourceKey.value !== resourceKey(schema, resource) && !window.confirm('Discard unsaved changes?')) return
  formDirty.value = false
  const url = new URL(window.location.href)
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

function showLogin() {
  if (window.location.pathname !== '/login') {
    intendedRoute = `${window.location.pathname}${window.location.search}${window.location.hash}`
    window.history.replaceState({}, '', '/login')
  }
  routeState.value = 'login'
}

function showApplication() {
  if (window.location.pathname === '/login') window.history.replaceState({}, '', intendedRoute || '/')
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
  restoreResourceFromURL()
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
  <LoginPage v-if="routeState === 'login'" :pending="authState.loginPending" :error="authState.loginError" @submit="submitLogin" />
  <main v-else-if="routeState === 'loading'" class="auth-state" role="status">
    <p class="eyebrow">Velmir</p>
    <h1>Checking your session…</h1>
  </main>
  <main v-else-if="routeState === 'error'" class="auth-state" role="alert">
    <p class="eyebrow">Connection error</p>
    <h1>Velmir is unavailable</h1>
    <p>{{ authState.error }}</p>
    <button type="button" class="primary-button" @click="initialize">Try again</button>
  </main>
  <div v-else class="app-layout">
    <aside class="sidebar">
      <div class="brand">
        <p class="eyebrow">PostgreSQL admin panel</p>
        <h1>Velmir</h1>
        <p>Metadata-driven database records</p>
      </div>

      <div class="sidebar-content">
        <p v-if="schemaViewState === 'loading'" class="sidebar-message">Loading schema…</p>
        <div v-else-if="schemaViewState === 'not_configured'" class="sidebar-message">
          Target database is not configured.
        </div>
        <div v-else-if="schemaViewState === 'empty'" class="sidebar-message">
          No tables or views found.
        </div>
        <div v-else-if="schemaViewState === 'forbidden'" class="sidebar-message sidebar-error" role="alert">
          You do not have permission to read any resources.
        </div>
        <div v-else-if="schemaViewState === 'error'" class="sidebar-message sidebar-error" role="alert">
          <p>Could not load the schema.</p>
          <button type="button" class="secondary-button" @click="loadSchema">Try again</button>
        </div>
        <nav v-else aria-label="Database resources">
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

      <section class="account" aria-label="Current user">
        <strong>{{ authState.user?.display_name }}</strong>
        <span>{{ authState.user?.email }}</span>
        <p v-if="authState.logoutError" class="account-error" role="alert">{{ authState.logoutError }}</p>
        <button type="button" class="logout-button" :disabled="authState.logoutPending" @click="submitLogout">
          {{ authState.logoutPending ? 'Signing out…' : 'Sign out' }}
        </button>
      </section>

      <section class="readiness" aria-label="Infrastructure readiness">
        <div>
          <span>Backend</span>
          <strong :class="`status-${viewState === 'error' ? 'unavailable' : viewState}`">
            {{ viewState === 'loading' ? 'Checking' : viewState === 'error' ? 'Unavailable' : 'Available' }}
          </strong>
        </div>
        <div>
          <span>Admin database</span>
          <strong :class="`status-${readiness?.admin_database ?? viewState}`">
            {{ databaseStateLabel(readiness?.admin_database) }}
          </strong>
        </div>
        <div>
          <span>Target database</span>
          <strong :class="`status-${readiness?.target_database ?? viewState}`">
            {{ databaseStateLabel(readiness?.target_database) }}
          </strong>
        </div>
        <button v-if="viewState === 'error'" type="button" class="readiness-retry" @click="loadReadiness">
          Retry readiness
        </button>
      </section>
    </aside>

    <main class="content">
      <template v-if="selectedResource">
        <header class="resource-header">
          <p class="eyebrow">{{ selectedSchemaName }}</p>
          <div class="resource-title">
            <h2>{{ selectedResource.name }}</h2>
            <span class="kind-badge">{{ selectedResource.kind }}</span>
          </div>
          <p>Metadata-driven records · {{ catalog?.write_enabled ? 'read and write' : 'read-only' }}</p>
        </header>
        <RecordsTable
          :backend-base-u-r-l="backendBaseURL"
          :schema-name="selectedSchemaName"
          :resource="selectedResource"
          :write-enabled="catalog?.write_enabled ?? false"
          :create-allowed="createAllowed"
          :update-allowed="updateAllowed"
          @dirty-change="formDirty = $event"
        />
      </template>

      <section v-else class="empty-state" :role="schemaViewState === 'forbidden' ? 'alert' : undefined">
        <p class="eyebrow">Database records</p>
        <h2 v-if="schemaViewState === 'loading'">Loading metadata…</h2>
        <h2 v-else-if="schemaViewState === 'not_configured'">Target database is not configured</h2>
        <h2 v-else-if="schemaViewState === 'empty'">No resources found</h2>
        <h2 v-else-if="schemaViewState === 'forbidden'">Forbidden</h2>
        <h2 v-else-if="schemaViewState === 'error'">Schema unavailable</h2>
        <h2 v-else>Select a resource</h2>
        <p v-if="schemaViewState === 'not_configured'">
          Set TARGET_DATABASE_URL and restart the backend to inspect a database.
        </p>
        <p v-else-if="schemaViewState === 'empty'">The target database has no user tables or views.</p>
        <p v-else-if="schemaViewState === 'forbidden'">Your account has no role granting read access. Contact an administrator.</p>
        <p v-else-if="schemaViewState === 'error'">Check the backend and target database readiness, then try again.</p>
        <p v-else-if="schemaViewState === 'ready'">Choose a table or view in the sidebar.</p>
      </section>
    </main>
  </div>
</template>
