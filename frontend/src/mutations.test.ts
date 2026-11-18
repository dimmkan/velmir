import { afterEach, describe, expect, it, vi } from 'vitest'
import { authState } from './auth'
import { buildRecordKey, createRecord, MutationRequestError, updateRecord } from './mutations'
import type { ResourceMetadata } from './schema'

const resource: ResourceMetadata = {
  name: 'links',
  kind: 'table',
  primary_key: ['book_id', 'category_id'],
  foreign_keys: [],
  columns: [],
}

describe('record mutations', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('builds an ordered composite JSON record key without numeric coercion', () => {
    expect(buildRecordKey(resource, { category_id: '9223372036854775807', book_id: '1', note: 'x' })).toBe(
      '{"book_id":"1","category_id":"9223372036854775807"}',
    )
  })

  it('posts values and returns the created record', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ record: { id: '9' } }), { status: 201 }))
    vi.stubGlobal('fetch', fetchMock)
    await expect(createRecord('', 'public', 'items', { id: '9223372036854775807' })).resolves.toEqual({ id: '9' })
    expect(fetchMock).toHaveBeenCalledWith('/api/resources/public/items/records', expect.objectContaining({
      method: 'POST',
      body: '{"values":{"id":"9223372036854775807"}}',
    }))
  })

  it('URL-encodes the record key and exposes server field errors', async () => {
    const key = buildRecordKey(resource, { book_id: '1', category_id: '2' })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      error: { code: 'CHECK_VIOLATION', message: 'Check failed', field_errors: { note: 'Invalid note' } },
    }), { status: 400 })))
    const request = updateRecord('', 'public', 'links', key, { note: 'bad' })
    await expect(request).rejects.toBeInstanceOf(MutationRequestError)
    await expect(request).rejects.toMatchObject({ code: 'CHECK_VIOLATION', fieldErrors: { note: 'Invalid note' } })
    expect(fetch).toHaveBeenCalledWith(`/api/resources/public/links/records/${encodeURIComponent(key)}`, expect.objectContaining({ method: 'PATCH' }))
  })

  it('sends the CSRF token and credentials with a mutation payload', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ record: { id: '9' } }), { status: 201 }))
    vi.stubGlobal('fetch', fetchMock)

    await createRecord('', 'public', 'items', { name: 'new' }, 'test-csrf-token')

    expect(fetchMock).toHaveBeenCalledWith('/api/resources/public/items/records', expect.objectContaining({
      method: 'POST',
      headers: expect.objectContaining({ 'X-CSRF-Token': 'test-csrf-token' }),
      credentials: 'same-origin',
      body: '{"values":{"name":"new"}}',
    }))
  })
})


describe('mutation authorization errors', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('preserves the authenticated state and valid CSRF flow on 403', async () => {
    authState.status = 'authenticated'
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      error: { code: 'FORBIDDEN', message: 'Permission denied' },
    }), { status: 403 }))
    vi.stubGlobal('fetch', fetchMock)

    const request = createRecord('', 'public', 'items', { name: 'x' }, 'valid-csrf-token')
    await expect(request).rejects.toMatchObject({ code: 'FORBIDDEN', status: 403 })
    expect(authState.status).toBe('authenticated')
    expect(fetchMock).toHaveBeenCalledWith('/api/resources/public/items/records', expect.objectContaining({
      headers: expect.objectContaining({ 'X-CSRF-Token': 'valid-csrf-token' }),
    }))
  })
})
