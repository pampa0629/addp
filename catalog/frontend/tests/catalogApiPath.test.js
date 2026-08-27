import { beforeEach, describe, expect, it, vi } from 'vitest'
import axios from 'axios'

const client = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  delete: vi.fn()
}))

vi.mock('../src/api/client', () => ({ default: client }))

import { getEntry, listEntries, listEntryFacets, replaceMyEntryMarks } from '../src/api/catalog'

describe('catalog frontend API paths', () => {
  beforeEach(() => vi.clearAllMocks())

  it('uses paths relative to the shared /api/v1 client base', async () => {
    await listEntries({ page: 1 })
    await listEntryFacets({ view: 'inventory' })
    await getEntry('entry/id')
    await replaceMyEntryMarks('entry/id', { favorite: true, following: false })

    expect(client.get).toHaveBeenNthCalledWith(1, '/catalog/entries', { params: { page: 1 } })
    expect(client.get).toHaveBeenNthCalledWith(2, '/catalog/entries/facets', { params: { view: 'inventory' } })
    expect(client.get).toHaveBeenNthCalledWith(3, '/catalog/entries/entry%2Fid')
    expect(client.put).toHaveBeenCalledWith('/catalog/me/entries/entry%2Fid/marks', { favorite: true, following: false })
    expect(axios.getUri({ baseURL: '/api/v1', url: client.get.mock.calls[0][0] })).toBe('/api/v1/catalog/entries')
  })
})
