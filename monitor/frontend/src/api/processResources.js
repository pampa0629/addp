import client from './client'

export const processResourcesAPI = {
  instances: (params, config) => client.get('/system/platform/module-instances', { ...config, params }),
  summaries: (ids, config) => client.get('/monitor/platform/process_resource_summaries', { ...config, params: { instance_ids: ids.join(',') } })
}
