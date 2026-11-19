import { afterEach, describe, expect, it, vi } from 'vitest'
import { authState } from './auth'
import { fetchRecordHistory, historyChanges, type HistoryEvent } from './history'
import type { ResourceMetadata } from './schema'

const resource: ResourceMetadata = {
  name: 'items',
  kind: 'table',
  primary_key: ['id'],
  foreign_keys: [],
  columns: [
    { name: 'id', data_type: 'bigint', nullable: false, default: null, primary_key: true, identity: '', generated: false, editable: false, enum_values: [] },
    { name: 'amount', data_type: 'numeric', nullable: true, default: null, primary_key: false, identity: '', generated: false, editable: true, enum_values: [] },
    { name: 'payload', data_type: 'jsonb', nullable: true, default: null, primary_key: false, identity: '', generated: false, editable: true, enum_values: [] },
  ],
}

function event(action: HistoryEvent['action']): HistoryEvent {
  return {
    id: 'event-1', actor: { id: 1, email: 'admin@example.test', display_name: 'Admin' },
    timestamp: '2025-01-02T03:04:05Z', action, schema: 'public', resource: 'items', record_key: { id: '1' },
    before: action === 'update' ? { id: '1', amount: '1.00', payload: { big: '9223372036854775807' } } : null,
    after: { id: '1', amount: '12345678901234567890.0001', payload: { big: '9223372036854775807' } },
  }
}

describe('history changes', () => {
  it('shows created values and only changed update fields with exact numeric strings', () => {
    expect(historyChanges(event('create'), resource).map((change) => change.column.name)).toEqual(['id', 'amount', 'payload'])
    const changes = historyChanges(event('update'), resource)
    expect(changes).toHaveLength(1)
    expect(changes[0].before).toBe('1.00')
    expect(changes[0].after).toBe('12345678901234567890.0001')
    expect(typeof changes[0].after).toBe('string')
  })
})

describe('history API', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    authState.status = 'authenticated'
  })

  it('loads URL-encoded composite key history with pagination', async () => {
    const body = { events: [event('update')], page: 2, limit: 10, has_next: false }
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    const key = '{"tenant_id":"7","id":"9223372036854775807"}'
    await expect(fetchRecordHistory('', 'public', 'items', key, 2, 10)).resolves.toEqual(body)
    expect(fetchMock).toHaveBeenCalledWith(
      `/api/resources/public/items/records/${encodeURIComponent(key)}/history?page=2&limit=10`,
      expect.objectContaining({ credentials: 'same-origin' }),
    )
  })

  it('keeps 403 distinct from session expiration and does not redirect to login', async () => {
    authState.status = 'authenticated'
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: { code: 'FORBIDDEN', message: 'Denied' } }), { status: 403 })))
    await expect(fetchRecordHistory('', 'private', 'secrets', '{"id":"1"}')).rejects.toMatchObject({ status: 403, code: 'FORBIDDEN' })
    expect(authState.status).toBe('authenticated')
  })

  it('expires the session on 401 and exposes server errors', async () => {
    authState.status = 'authenticated'
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ error: { code: 'UNAUTHENTICATED', message: 'Session required' } }), { status: 401 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ error: { code: 'HISTORY_QUERY_FAILED', message: 'Failed' } }), { status: 500 })))
    await expect(fetchRecordHistory('', 'public', 'items', '{"id":"1"}')).rejects.toMatchObject({ status: 401 })
    expect(authState.status).toBe('anonymous')
    await expect(fetchRecordHistory('', 'public', 'items', '{"id":"1"}')).rejects.toMatchObject({ status: 500, code: 'HISTORY_QUERY_FAILED' })
  })
})
