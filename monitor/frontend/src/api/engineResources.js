import client from './client'

export const engineResourcesAPI = {
  deployments: (params, config) => client.get('/system/platform/engine_deployments', { ...config, params }),
  deployment: (id, config) => client.get(`/system/platform/engine_deployments/${encodeURIComponent(id)}`, config),
  summaries: (ids, config) => client.get('/monitor/platform/engine_resource_summaries', { ...config, params: { deployment_ids: ids.join(',') } }),
  instant: (params, config) => client.get('/monitor/platform/engine_resource_observations', { ...config, params }),
  trend: (params, config) => client.get('/monitor/platform/engine_resource_trends', { ...config, params })
}
