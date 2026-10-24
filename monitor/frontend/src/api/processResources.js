import client from './client'

export const processResourcesAPI = {
  container: (id, config) => client.get('/monitor/platform/container_resource_observations', { ...config, params: { instance_ids: String(id) } }),
  instances: (params, config) => client.get('/system/platform/module-instances', { ...config, params }),
  summaries: (ids, config) => client.get('/monitor/platform/process_resource_summaries', { ...config, params: { instance_ids: ids.join(',') } }),
  trend: (params, config) => client.get('/monitor/platform/process_resource_trends', { ...config, params })
}
