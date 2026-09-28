import client from './client'

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
