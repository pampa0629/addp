import client from './client'

export const cdcResourcesAPI = {
  instant: config => client.get('/monitor/platform/cdc_resource_observations', config),
  trend: (params, config) => client.get('/monitor/platform/cdc_resource_trends', { ...config, params })
}
