import client from './client'

export const processResourcesAPI = {
  frontendNodes: config => client.get('/monitor/platform/frontend_nodes', config),
  frontend: (nodeID, config) => client.get('/monitor/platform/frontend_observations', { ...config, params: { node_id: nodeID } }),
  container: (id, config) => client.get('/monitor/platform/container_resource_observations', { ...config, params: { instance_ids: String(id) } }),
  instances: (params, config) => client.get('/system/platform/module-instances', { ...config, params }),
  startupFailures: config => client.get('/system/platform/module_startup_failures', config),
  summaries: (ids, config) => client.get('/monitor/platform/process_resource_summaries', { ...config, params: { instance_ids: ids.join(',') } }),
  totals: config => client.get('/monitor/platform/process_resource_totals', config),
  trend: (params, config) => client.get('/monitor/platform/process_resource_trends', { ...config, params })
}
