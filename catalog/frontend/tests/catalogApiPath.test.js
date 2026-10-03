import { beforeEach, describe, expect, it, vi } from 'vitest'
import axios from 'axios'

const client = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  delete: vi.fn()
}))

vi.mock('../src/api/client', () => ({ default: client }))

import { batchGovernance, createStandardMapping, deleteStandardMapping, exportEntryDataDictionary, getEntry, getEntryDataDictionary, getGovernanceCoverage, listDomainOverviews, listEntries, listEntryFacets, listMyProjectGroups, listReferenceCandidates, listStandardMappingRevisionOptions, replaceMyEntryMarks, resolveSourceEntries, reviewStandardMapping, updateEntryGovernance, updateStandardMapping } from '../src/api/catalog'
import { listDomainElements, listDomainGlossaries, listDomainMetrics } from '../src/api/standard'
import { listDomainQualityIssues, listDomainQualityPlans, listDomainQualityRules } from '../src/api/quality'
import { domainStandardQuery } from '../src/utils/domainStandardSummary'
import { domainQualityQuery } from '../src/utils/domainQualitySummary'
import { transferEntryResponsibilities } from '../src/api/catalog'
import { createSharingDecision, getSharingDecision, getSharingRequest, listSharingRecipients, listSharingRequests, listSharingDecisions, observeSharingRequirement, prepareSharingRequest } from '../src/api/catalog'

describe('catalog frontend API paths', () => {
  beforeEach(() => vi.clearAllMocks())

  it('observes only the exact target under the User client before a separate explicit preparation command', async () => {
    const target = { engine_id: '9007199254740993', version: 'v1', segments: [{ name: 'root' }, { name: 'table' }] }
    const payload = { request_id: 'request', decision_id: 'decision', requirement_version: '9007199254740993' }
    await listSharingDecisions('entry/id', { page: 2, page_size: 20 })
    await observeSharingRequirement(target)
    await prepareSharingRequest('entry/id', payload)
    expect(client.get).toHaveBeenCalledExactlyOnceWith('/catalog/entries/entry%2Fid/sharing_decision_candidates', { params: { page: 2, page_size: 20 } })
    expect(client.post.mock.calls).toEqual([
      ['/system/engines/9007199254740993/access_handling_requirement', { version: target.version, segments: target.segments }],
      ['/catalog/entries/entry%2Fid/sharing_fulfillments', payload]
    ])
  })

  it('separates explicit confirmation POST from read-only confirmation and request recovery', async () => {
    const payload = { decision_id: 'opaque', version: '9007199254740993' }
    await createSharingDecision('entry/id', payload)
    await listSharingRecipients('entry/id', { recipient_type: 'project_group', page: 1 })
    await getSharingDecision('entry/id', 'decision/id')
    await listSharingRequests('entry/id', { page: 2, page_size: 20 })
    await getSharingRequest('entry/id', 'request/id')
    expect(client.post).toHaveBeenCalledExactlyOnceWith('/catalog/entries/entry%2Fid/sharing_decisions', payload)
    expect(client.get).toHaveBeenCalledWith('/catalog/entries/entry%2Fid/sharing_recipient_candidates', { params: { recipient_type: 'project_group', page: 1 } })
    expect(client.get).toHaveBeenCalledWith('/catalog/entries/entry%2Fid/sharing_decisions/decision%2Fid')
    expect(client.get).toHaveBeenCalledWith('/catalog/entries/entry%2Fid/sharing_fulfillments', { params: { page: 2, page_size: 20 } })
    expect(client.get).toHaveBeenCalledWith('/catalog/entries/entry%2Fid/sharing_fulfillments/request%2Fid')
  })

  it('uses a responsibility-only subresource for deprecated entries', async () => {
    const payload = { version: 3, reason: 'Transfer', responsibilities: [] }
    await transferEntryResponsibilities('entry/id', payload)
    expect(client.put).toHaveBeenCalledWith('/catalog/entries/entry%2Fid/responsibilities', payload)
  })

  it('reads domain overviews through Catalog, not Standard user APIs', async () => {
    await listDomainOverviews()
    expect(client.get).toHaveBeenCalledWith('/catalog/domains')
  })

  it('reads domain-owned professional definitions directly from Standard with exact pagination', async () => {
    const params = domainStandardQuery('42', 2)
    await listDomainGlossaries(params)
    await listDomainElements(params)
    await listDomainMetrics(params)
    expect(params).toEqual({ scope_type: 'domain', owner_domain_id: '42', page: 2, page_size: 5 })
    expect(client.get.mock.calls).toEqual([
      ['/standard/glossaries', { params }],
      ['/standard/elements', { params }],
      ['/standard/metrics', { params }]
    ])
  })

  it('reads domain-owned quality governance directly from Quality with the current user client', async () => {
    const rules = domainQualityQuery('42', 'rules')
    const plans = domainQualityQuery('42', 'plans')
    const issues = domainQualityQuery('42', 'issues')
    await listDomainQualityRules(rules)
    await listDomainQualityPlans(plans)
    await listDomainQualityIssues(issues)
    expect(client.get.mock.calls).toEqual([
      ['/quality/rules', { params: rules }],
      ['/quality/plans', { params: plans }],
      ['/quality/issues', { params: issues }]
    ])
  })

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

  it('uses only the independent StandardMapping candidate and review routes', async () => {
    const payload = { catalog_entry_id: 'entry', component_id: 'component', element_id: '50', element_revision_id: '501' }
    await listStandardMappingRevisionOptions('50')
    await createStandardMapping(payload)
    await updateStandardMapping('mapping/id', { ...payload, version: 1 })
    await deleteStandardMapping('mapping/id', 2)
    await reviewStandardMapping('mapping/id', 'approve', { version: 3, opinion: '' })

    expect(client.get).toHaveBeenCalledWith('/catalog/standard-mappings/revision-options', { params: { element_id: '50' } })
    expect(client.post).toHaveBeenCalledWith('/catalog/standard-mappings', payload)
    expect(client.put).toHaveBeenCalledWith('/catalog/standard-mappings/mapping%2Fid', { ...payload, version: 1 })
    expect(client.delete).toHaveBeenCalledWith('/catalog/standard-mappings/mapping%2Fid', { data: { version: 2 } })
    expect(client.post).toHaveBeenCalledWith('/catalog/standard-mappings/mapping%2Fid/approve', { version: 3, opinion: '' })
    await expect(reviewStandardMapping('mapping/id', 'certify', { version: 3 })).rejects.toThrow('invalid standard mapping action')
  })
})
