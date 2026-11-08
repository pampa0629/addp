import client from './client'

export function getEmbeddingConfiguration() {
  return client.get('/manager/settings/embedding')
}

export function updateEmbeddingConfiguration(payload) {
  return client.put('/manager/settings/embedding', payload)
}

export function getInferenceBinding(scenario = 'semantic_search_embedding') {
  return client.get(`/manager/settings/inference-bindings/${encodeURIComponent(scenario)}`)
}

export function updateInferenceBinding(payload, scenario = 'semantic_search_embedding') {
  return client.put(`/manager/settings/inference-bindings/${encodeURIComponent(scenario)}`, payload)
}

export function listInferenceModelLabels() {
  return client.get('/inference/model-labels')
}

export function listInferenceProfiles() {
  return client.get('/inference/model-profiles', { params: { page: 1, page_size: 100 } })
}

export function listInferenceDeployments() {
  return client.get('/inference/model-deployments', { params: { page: 1, page_size: 100 } })
}

export function getQuickViewPolicy() {
  return client.get('/manager/settings/quick-view-policy')
}

export function updateQuickViewPolicy(payload) {
  return client.put('/manager/settings/quick-view-policy', payload)
}
