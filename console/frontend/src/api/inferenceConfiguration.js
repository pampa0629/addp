import client from './client'

export function listInferenceProfiles() {
  return client.get('/inference/model-profiles', { params: { page: 1, page_size: 100 } })
}

export function getInferenceBinding(owner, scenarioCode) {
  return client.get(`/${owner}/settings/inference-bindings/${scenarioCode}`)
}

export function updateInferenceBinding(owner, scenarioCode, payload) {
  return client.put(`/${owner}/settings/inference-bindings/${scenarioCode}`, payload)
}
