import client from './client'

export const engineResourcesAPI = {
  deployments: (params, config) => client.get('/system/platform/engine_deployments', { ...config, params }),
  deployment: (id, config) => client.get(`/system/platform/engine_deployments/${encodeURIComponent(id)}`, config),
  updateDeployment: (id, body, config) => client.put(`/system/platform/engine_deployments/${encodeURIComponent(id)}`, body, config),
  summaries: (ids, config) => client.get('/monitor/platform/engine_resource_summaries', { ...config, params: { deployment_ids: ids.join(',') } }),
  instant: (params, config) => client.get('/monitor/platform/engine_resource_observations', { ...config, params }),
  nfs: (nodeID, config) => client.get('/monitor/platform/business_nfs_observations', { ...config, params: { node_id: nodeID } }),
  runtimeSources: config => client.get('/monitor/platform/engine_runtime_observations/sources', config),
  runtime: (params, config) => client.get('/monitor/platform/engine_runtime_observations', { ...config, params }),
  trend: (params, config) => client.get('/monitor/platform/engine_resource_trends', { ...config, params })
}
