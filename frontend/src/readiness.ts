export type DatabaseState = 'ready' | 'unavailable' | 'not_configured'

export interface ReadinessResponse {
  status: 'ready' | 'not_ready'
  admin_database: DatabaseState
  target_database: DatabaseState
}

const databaseStates: DatabaseState[] = ['ready', 'unavailable', 'not_configured']

function isDatabaseState(value: unknown): value is DatabaseState {
  return databaseStates.includes(value as DatabaseState)
}

function isReadinessResponse(value: unknown): value is ReadinessResponse {
  if (typeof value !== 'object' || value === null) return false

  const response = value as Record<string, unknown>
  return (
    (response.status === 'ready' || response.status === 'not_ready') &&
    isDatabaseState(response.admin_database) &&
    isDatabaseState(response.target_database)
  )
}

export async function fetchReadiness(backendBaseURL: string, signal?: AbortSignal) {
  const response = await fetch(`${backendBaseURL}/readyz`, {
    headers: { Accept: 'application/json' },
    signal,
  })
  const body: unknown = await response.json()

  if (!isReadinessResponse(body)) {
    throw new Error('Invalid readiness response')
  }

  return body
}
