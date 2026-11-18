import client, { explicitWriteClient } from './client'
const endpoint = '/system/platform/engine_deployments'
export const engineDeploymentsAPI = {
  list: (params, config) => client.get(endpoint, { ...config, params }),
  get: (id, config) => client.get(`${endpoint}/${encodeURIComponent(id)}`, config),
  discover: config => client.get(`${endpoint}/discovery`, config),
  summaries: (ids, config) => client.get('/monitor/platform/engine_resource_summaries', { ...config, params: { deployment_ids: ids.join(',') } }),
  create: (body, config) => explicitWriteClient.post(endpoint, body, config),
  update: (id, body, config) => explicitWriteClient.put(`${endpoint}/${encodeURIComponent(id)}`, body, config)
}
