import client from './client'

export const nodeResourcesAPI = {
  nodes: (params, config) => client.get('/system/platform/host_nodes', { ...config, params }),
  node: (id, config) => client.get(`/system/platform/host_nodes/${encodeURIComponent(id)}`, config),
  summaries: (ids, config) => client.get('/monitor/platform/resource_summaries', { ...config, params: { node_ids: ids.join(',') } }),
  instant: (params, config) => client.get('/monitor/platform/resource_observations', { ...config, params }),
  trend: (params, config) => client.get('/monitor/platform/resource_trends', { ...config, params })
}
