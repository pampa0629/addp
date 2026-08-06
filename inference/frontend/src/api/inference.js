import client from './client'

const base = '/inference'

export const providerTemplateAPI = {
  list: () => client.get(`${base}/provider-templates`)
}

export const providerAPI = {
  list: (params) => client.get(`${base}/provider-connections`, { params }),
  create: (data) => client.post(`${base}/provider-connections`, data),
  update: (id, data) => client.put(`${base}/provider-connections/${id}`, data),
  remove: (id) => client.delete(`${base}/provider-connections/${id}`),
  setCredential: (id, credential) => client.put(`${base}/provider-connections/${id}/credential`, { credential }),
  deleteCredential: (id) => client.delete(`${base}/provider-connections/${id}/credential`),
  discoverModels: (id) => client.post(`${base}/provider-connections/${id}/discover-models`)
}

export const deploymentAPI = {
  list: (params) => client.get(`${base}/model-deployments`, { params }),
  create: (data) => client.post(`${base}/model-deployments`, data),
  update: (id, data) => client.put(`${base}/model-deployments/${id}`, data),
  remove: (id) => client.delete(`${base}/model-deployments/${id}`),
  probe: (id) => client.post(`${base}/model-deployments/${id}/probe`)
}

export const profileAPI = {
  list: (params) => client.get(`${base}/model-profiles`, { params }),
  create: (data) => client.post(`${base}/model-profiles`, data),
  update: (id, data) => client.put(`${base}/model-profiles/${id}`, data)
}
