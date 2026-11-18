import { reactive, isProxy } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { buildCreatePayload, cloneFormState, initialFormState, validateRecordForm } from './recordForm'
import { createRecord } from './mutations'
import type { ResourceMetadata } from './schema'

const authors: ResourceMetadata = {
  name: 'authors',
  kind: 'table',
  primary_key: ['id'],
  foreign_keys: [],
  columns: [
    { name: 'id', data_type: 'bigint', nullable: false, default: "nextval('authors_id_seq')", primary_key: true, identity: 'always', generated: false, editable: false, enum_values: [] },
    { name: 'name', data_type: 'character varying(200)', nullable: false, default: null, primary_key: false, identity: '', generated: false, editable: true, enum_values: [] },
    { name: 'bio', data_type: 'text', nullable: true, default: null, primary_key: false, identity: '', generated: false, editable: true, enum_values: [] },
    { name: 'created_at', data_type: 'timestamp with time zone', nullable: false, default: 'now()', primary_key: false, identity: '', generated: false, editable: true, enum_values: [] },
  ],
}

describe('reactive create form regression', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('creates once from reactive state and handles HTTP 201 without cloning proxies', async () => {
    const state = reactive(initialFormState('create', authors))
    state.name.value = 'Alice'
    state.bio.isNull = false
    state.bio.value = ''
    const payload = buildCreatePayload(authors, state)

    expect(isProxy(state.name)).toBe(true)
    expect(isProxy(payload)).toBe(false)
    expect(payload).toEqual({ name: 'Alice', bio: '' })

    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      record: { id: '4', name: 'Alice', bio: '', created_at: '2025-01-02T03:04:05Z' },
    }), { status: 201 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(createRecord('', 'public', 'authors', payload)).resolves.toMatchObject({ id: '4', name: 'Alice' })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ values: { name: 'Alice', bio: '' } })

    const snapshot = cloneFormState(state)
    expect(snapshot).toEqual({
      id: { value: '', isNull: false, useDefault: true },
      name: { value: 'Alice', isNull: false, useDefault: false },
      bio: { value: '', isNull: false, useDefault: false },
      created_at: { value: '', isNull: false, useDefault: true },
    })
    expect(isProxy(snapshot)).toBe(false)
    expect(isProxy(snapshot.name)).toBe(false)
  })

  it('blocks an invalid submit without any HTTP request', async () => {
    const state = reactive(initialFormState('create', authors))
    state.name.value = '   '
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    const errors = validateRecordForm('create', authors, state)
    if (Object.keys(errors).length === 0) {
      await createRecord('', 'public', 'authors', buildCreatePayload(authors, state))
    }

    expect(errors).toEqual({ name: 'This field is required' })
    expect(fetchMock).toHaveBeenCalledTimes(0)
    expect(state.name.value).toBe('   ')
  })
})
