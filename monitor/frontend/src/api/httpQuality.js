import client from './client'

export const httpQualityAPI = {
  instances: (params, config) => client.get('/system/platform/module-instances', { ...config, params }),
  summaries: (ids, window, config) => client.get('/monitor/platform/http_observations', { ...config, params: { instance_ids: ids.join(','), window_seconds: window } }),
  interfaces: (id, window, sort, config) => client.get('/monitor/platform/http_interfaces', { ...config, params: { instance_ids: id, window_seconds: window, sort } }),
  trend: (params, config) => client.get('/monitor/platform/http_trends', { ...config, params })
}
