import client from './client'
const endpoint = '/system/platform/engine_deployments'
export const engineDeploymentsAPI = {
  list: (params, config) => client.get(endpoint, { ...config, params }),
  get: (id, config) => client.get(`${endpoint}/${encodeURIComponent(id)}`, config),
  create: (body, config) => client.post(endpoint, body, config),
  update: (id, body, config) => client.put(`${endpoint}/${encodeURIComponent(id)}`, body, config)
}
