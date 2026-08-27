import client from './client'

export async function listEntries(params) {
	return client.get('/catalog/entries', { params })
}

export async function listEntryFacets(params) {
	return client.get('/catalog/entries/facets', { params })
}

export async function getEntry(id) {
	return client.get(`/catalog/entries/${encodeURIComponent(id)}`)
}

export async function updateEntry(id, payload) {
	return client.put(`/catalog/entries/${encodeURIComponent(id)}`, payload)
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

export async function listMyEntries(params) {
	return client.get('/catalog/me/entries', { params })
}

export async function getMyEntryMarks(id) {
	return client.get(`/catalog/me/entries/${encodeURIComponent(id)}/marks`)
}

export async function replaceMyEntryMarks(id, payload) {
	return client.put(`/catalog/me/entries/${encodeURIComponent(id)}/marks`, payload)
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
