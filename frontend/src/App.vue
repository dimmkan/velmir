<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { fetchReadiness, type DatabaseState, type ReadinessResponse } from './readiness'

type ViewState = 'loading' | 'ready' | 'error'

const backendBaseURL = (import.meta.env.VITE_BACKEND_BASE_URL ?? '').replace(/\/+$/, '')
const viewState = ref<ViewState>('loading')
const readiness = ref<ReadinessResponse | null>(null)

const databaseStateLabels: Record<DatabaseState, string> = {
  ready: 'Ready',
  unavailable: 'Unavailable',
  not_configured: 'Not configured',
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

onMounted(loadReadiness)
</script>

<template>
  <main class="app-shell">
    <section class="status-card" aria-labelledby="app-title">
      <p class="eyebrow">PostgreSQL admin panel</p>
      <div class="heading-row">
        <h1 id="app-title">Velmir</h1>
        <span
          v-if="viewState === 'ready'"
          class="overall-status"
          :class="`overall-status-${readiness?.status}`"
        >
          {{ readiness?.status === 'ready' ? 'Ready' : 'Not ready' }}
        </span>
      </div>
      <p class="description">Infrastructure readiness</p>

      <dl class="status-list" aria-live="polite">
        <div class="status-row">
          <dt>Backend</dt>
          <dd :class="`status-${viewState === 'error' ? 'unavailable' : viewState}`">
            {{ viewState === 'loading' ? 'Checking' : viewState === 'error' ? 'Unavailable' : 'Available' }}
          </dd>
        </div>
        <div class="status-row">
          <dt>Admin database</dt>
          <dd :class="`status-${readiness?.admin_database ?? viewState}`">
            {{ databaseStateLabel(readiness?.admin_database) }}
          </dd>
        </div>
        <div class="status-row">
          <dt>Target database</dt>
          <dd :class="`status-${readiness?.target_database ?? viewState}`">
            {{ databaseStateLabel(readiness?.target_database) }}
          </dd>
        </div>
      </dl>

      <div v-if="viewState === 'error'" class="error-message" role="alert">
        <p>Could not load backend readiness. Check that the backend is running and the base URL is correct.</p>
        <button type="button" @click="loadReadiness">Try again</button>
      </div>
    </section>
  </main>
</template>
