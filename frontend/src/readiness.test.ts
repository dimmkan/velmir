import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchReadiness, type ReadinessResponse } from './readiness'

function mockReadinessResponse(status: number, body: ReadinessResponse) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify(body), {
        status,
        headers: { 'Content-Type': 'application/json' },
      }),
    ),
  )
}

describe('fetchReadiness', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('accepts HTTP 200 with ready status', async () => {
    const body: ReadinessResponse = {
      status: 'ready',
      admin_database: 'ready',
      target_database: 'ready',
    }
    mockReadinessResponse(200, body)

    await expect(fetchReadiness('http://127.0.0.1:8080')).resolves.toEqual(body)
  })

  it('accepts HTTP 503 with not_ready status and database states', async () => {
    const body: ReadinessResponse = {
      status: 'not_ready',
      admin_database: 'unavailable',
      target_database: 'not_configured',
    }
    mockReadinessResponse(503, body)

    await expect(fetchReadiness('http://127.0.0.1:8080')).resolves.toEqual(body)
  })
})
