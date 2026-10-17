import client from './client'
import { serializeEngineCatalogTarget } from '@common-ui'
import { isSharingInt64 } from '../utils/sharingConfirmation'

export async function listEntries(params) {
	return client.get('/catalog/entries', { params })
}

export async function listDomainOverviews() {
	return client.get('/catalog/domains')
}

export async function listEntryFacets(params) {
	return client.get('/catalog/entries/facets', { params })
}

export async function resolveSourceEntries(references) {
	return client.post('/catalog/entries/resolve-sources', { references })
}

export async function batchGovernance(payload) {
	return client.post('/catalog/entries/batch_governance', payload)
}

export async function listReferenceCandidates(params) {
	return client.get('/catalog/reference-candidates', { params })
}

export async function getEntry(id) {
	return client.get(`/catalog/entries/${encodeURIComponent(id)}`)
}

export async function listSharingRecipients(id, params) {
	return client.get(`/catalog/entries/${encodeURIComponent(id)}/sharing_recipient_candidates`, { params })
}

export async function createSharingDecision(id, payload) {
	return client.post(`/catalog/entries/${encodeURIComponent(id)}/sharing_decisions`, payload)
}

export async function getSharingBoundary(id) {
  return client.get(`/catalog/entries/${encodeURIComponent(id)}/sharing_boundary`)
}

export async function getSharingDecision(id, decisionID) {
	return client.get(`/catalog/entries/${encodeURIComponent(id)}/sharing_decisions/${encodeURIComponent(decisionID)}`)
}

export async function listSharingConfirmations(id, params) {
  return client.get(`/catalog/entries/${encodeURIComponent(id)}/sharing_decisions`, { params })
}

export async function listSharingConfirmationResults(id, decisionID, params) {
  return client.get(`/catalog/entries/${encodeURIComponent(id)}/sharing_decisions/${encodeURIComponent(decisionID)}/fulfillments`, { params })
}

export async function listSharingRequests(id, params) {
	return client.get(`/catalog/entries/${encodeURIComponent(id)}/sharing_fulfillments`, { params })
}

export async function listSharingDecisions(id, params) {
	return client.get(`/catalog/entries/${encodeURIComponent(id)}/sharing_decision_candidates`, { params })
}

// Current User client; never use the Catalog runtime identity for this read.
export async function observeSharingRequirement(target) {
	return client.post(`/system/engines/${encodeURIComponent(target.engine_id)}/access_handling_requirement`, {
		version: target.version, segments: target.segments
	})
}

// Read the direct relation, not a user's combined department/project sources.
// This optional User observation never replaces System's write-time guard.
export async function findExistingSharingGrant(candidate) {
  if (!['user', 'project_group'].includes(candidate?.recipient_type) || !isSharingInt64(candidate?.recipient_id) ||
      !/^[0-9a-f]{64}$/.test(candidate?.item_fingerprint || '')) throw new Error('invalidSharingGrantObservation')
  const result = await client.get(`/system/engines/${encodeURIComponent(candidate.target.engine_id)}/access_grants/history`, { params: {
    catalog_path: serializeEngineCatalogTarget(candidate.target), recipient_type: candidate.recipient_type,
    recipient_id: candidate.recipient_id, state: 'active', page: 1, page_size: 100
  } })
  if (!Array.isArray(result?.data) || !Number.isSafeInteger(result.total) || result.total < result.data.length ||
      result.data.some(row => row.recipient_type !== candidate.recipient_type || row.recipient_id !== candidate.recipient_id ||
        row.action !== 'read' || row.revocation !== null || !/^[0-9a-f]{64}$/.test(row.item_fingerprint) ||
        (row.expiry_mode !== 'until_revoked' && row.expiry_mode !== 'at_time') ||
        (row.expiry_mode === 'until_revoked' ? row.expires_at !== null : !row.expires_at || !Number.isFinite(new Date(row.expires_at).getTime())))) {
    throw new Error('invalidSharingGrantObservation')
  }
  const existing = result.data.find(row => row.item_fingerprint === candidate.item_fingerprint)
  if (existing) return existing
  if (result.total !== result.data.length) throw new Error('incompleteSharingGrantObservation')
  return null
}

export async function prepareSharingRequest(id, payload) {
	return client.post(`/catalog/entries/${encodeURIComponent(id)}/sharing_fulfillments`, payload)
}

export async function initializeSharingRequirement(target, serializedPayload) {
	return client.post(`/system/engines/${encodeURIComponent(target.engine_id)}/access_approval_requirements`, serializedPayload, {
		headers: { 'Content-Type': 'application/json' }
	})
}

export async function getSharingRequest(id, requestID) {
	return client.get(`/catalog/entries/${encodeURIComponent(id)}/sharing_fulfillments/${encodeURIComponent(requestID)}`)
}

export async function getEntryDataDictionary(id, asOf) {
	const params = asOf ? { as_of: asOf } : undefined
	return client.get(`/catalog/entries/${encodeURIComponent(id)}/data-dictionary`, { params })
}

export async function exportEntryDataDictionary(id, asOf) {
	const params = asOf ? { as_of: asOf } : undefined
	return client.get(`/catalog/entries/${encodeURIComponent(id)}/data-dictionary/export`, {
		params,
		responseType: 'blob'
	})
}

export async function updateEntry(id, payload) {
	return client.put(`/catalog/entries/${encodeURIComponent(id)}`, payload)
}

export async function updateEntryGovernance(id, payload) {
	return client.put(`/catalog/entries/${encodeURIComponent(id)}/governance`, payload)
}

export async function transferEntryResponsibilities(id, payload) {
	return client.put(`/catalog/entries/${encodeURIComponent(id)}/responsibilities`, payload)
}

export async function listStandardMappingRevisionOptions(elementId) {
	return client.get('/catalog/standard-mappings/revision-options', { params: { element_id: elementId } })
}

export async function createStandardMapping(payload) {
	return client.post('/catalog/standard-mappings', payload)
}

export async function updateStandardMapping(id, payload) {
	return client.put(`/catalog/standard-mappings/${encodeURIComponent(id)}`, payload)
}

export async function deleteStandardMapping(id, version) {
	return client.delete(`/catalog/standard-mappings/${encodeURIComponent(id)}`, { data: { version } })
}

export async function reviewStandardMapping(id, action, payload) {
	if (!['approve', 'reject', 'withdraw'].includes(action)) throw new Error('invalid standard mapping action')
	return client.post(`/catalog/standard-mappings/${encodeURIComponent(id)}/${action}`, payload)
}

export async function rebindSource(id, payload) {
	return client.post(`/catalog/entries/${encodeURIComponent(id)}/rebind-source`, payload)
}

export async function getEntryHistory(id) {
	return client.get(`/catalog/entries/${encodeURIComponent(id)}/history`)
}

export async function listGovernanceTasks(params) {
	return client.get('/catalog/governance/tasks', { params })
}

export async function getGovernanceCoverage() {
	return client.get('/catalog/governance/coverage')
}

export async function listMyEntries(params) {
	return client.get('/catalog/me/entries', { params })
}

export async function getMyEntryMarks(id) {
	return client.get(`/catalog/me/entries/${encodeURIComponent(id)}/marks`)
}

export async function replaceMyEntryMarks(id, payload) {
	return client.put(`/catalog/me/entries/${encodeURIComponent(id)}/marks`, payload)
}

export async function listMyProjectGroups() {
	return client.get('/catalog/me/project-groups')
}

export async function listCollections(params) {
	return client.get('/catalog/collections', { params })
}

export async function createCollection(payload) {
	return client.post('/catalog/collections', payload)
}

export async function getCollection(id) {
	return client.get(`/catalog/collections/${encodeURIComponent(id)}`)
}

export async function updateCollection(id, payload) {
	return client.put(`/catalog/collections/${encodeURIComponent(id)}`, payload)
}

export async function deleteCollection(id, version) {
	return client.delete(`/catalog/collections/${encodeURIComponent(id)}`, { data: { version } })
}
