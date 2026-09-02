import { beforeEach, describe, expect, it, vi } from 'vitest'
import axios from 'axios'

const client = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  delete: vi.fn()
}))

vi.mock('../src/api/client', () => ({ default: client }))

import { batchGovernance, exportEntryDataDictionary, getEntry, getEntryDataDictionary, getGovernanceCoverage, listEntries, listEntryFacets, listMyProjectGroups, listReferenceCandidates, replaceMyEntryMarks, resolveSourceEntries, updateEntryGovernance } from '../src/api/catalog'

describe('catalog frontend API paths', () => {
  beforeEach(() => vi.clearAllMocks())

  it('uses paths relative to the shared /api/v1 client base', async () => {
    await listEntries({ page: 1 })
    await listEntryFacets({ view: 'inventory' })
    await getGovernanceCoverage()
    await resolveSourceEntries([{ source_module: 'model', source_type: 'entity', source_identity: '1' }])
    await batchGovernance({ entries: [], operation: 'assign_primary_domain', reference_id: '1' })
    await listReferenceCandidates({ reference_type: 'domain', search: 'sales' })
    await listMyProjectGroups()
    await getEntry('entry/id')
	await getEntryDataDictionary('entry/id', '2026-08-28T10:00:00.000Z')
    await exportEntryDataDictionary('entry/id', '2026-08-28T10:00:00.000Z')
    await replaceMyEntryMarks('entry/id', { favorite: true, following: false })
    await updateEntryGovernance('entry/id', { version: 3, governance_status: 'certified' })

    expect(client.get).toHaveBeenNthCalledWith(1, '/catalog/entries', { params: { page: 1 } })
    expect(client.get).toHaveBeenNthCalledWith(2, '/catalog/entries/facets', { params: { view: 'inventory' } })
    expect(client.get).toHaveBeenNthCalledWith(3, '/catalog/governance/coverage')
    expect(client.post).toHaveBeenCalledWith('/catalog/entries/resolve-sources', { references: [{ source_module: 'model', source_type: 'entity', source_identity: '1' }] })
    expect(client.post).toHaveBeenCalledWith('/catalog/entries/batch_governance', { entries: [], operation: 'assign_primary_domain', reference_id: '1' })
    expect(client.get).toHaveBeenNthCalledWith(4, '/catalog/reference-candidates', { params: { reference_type: 'domain', search: 'sales' } })
    expect(client.get).toHaveBeenNthCalledWith(5, '/catalog/me/project-groups')
    expect(client.get).toHaveBeenNthCalledWith(6, '/catalog/entries/entry%2Fid')
	expect(client.get).toHaveBeenNthCalledWith(7, '/catalog/entries/entry%2Fid/data-dictionary', { params: { as_of: '2026-08-28T10:00:00.000Z' } })
    expect(client.get).toHaveBeenNthCalledWith(8, '/catalog/entries/entry%2Fid/data-dictionary/export', {
      params: { as_of: '2026-08-28T10:00:00.000Z' },
      responseType: 'blob'
    })
    expect(client.put).toHaveBeenCalledWith('/catalog/me/entries/entry%2Fid/marks', { favorite: true, following: false })
    expect(client.put).toHaveBeenCalledWith('/catalog/entries/entry%2Fid/governance', { version: 3, governance_status: 'certified' })
    expect(axios.getUri({ baseURL: '/api/v1', url: client.get.mock.calls[0][0] })).toBe('/api/v1/catalog/entries')
  })
})
