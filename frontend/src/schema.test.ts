import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchSchema, SchemaRequestError, type SchemaResponse } from './schema'

describe('fetchSchema', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('loads schema metadata', async () => {
    const body: SchemaResponse = {
      write_enabled: true,
      schemas: [
        {
          name: 'public',
          resources: [
            {
              name: 'items',
              kind: 'table',
              columns: [{ name: 'id', data_type: 'bigint', nullable: false, default: null, primary_key: true, identity: 'always', generated: false, editable: false, enum_values: [] }],
              primary_key: ['id'],
              foreign_keys: [],
            },
          ],
        },
      ],
    }
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200 })))

    await expect(fetchSchema('')).resolves.toEqual(body)
  })

  it('returns TARGET_NOT_CONFIGURED from an error response', async () => {
    const body = { error: { code: 'TARGET_NOT_CONFIGURED', message: 'Target database is not configured' } }
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 503 })))

    const request = fetchSchema('')
    await expect(request).rejects.toBeInstanceOf(SchemaRequestError)
    await expect(request).rejects.toMatchObject({ code: 'TARGET_NOT_CONFIGURED' })
  })
})
