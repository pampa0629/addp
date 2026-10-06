import client from './client'

const endpoint = '/monitor/platform/monitoring_targets'
export const monitoringTargetsAPI = {
  list: (params, config) => client.get(endpoint, { ...config, params }),
  get: (id, config) => client.get(`${endpoint}/${encodeURIComponent(id)}`, config),
  create: (body, config) => client.post(endpoint, body, config),
  update: (id, body, config) => client.put(`${endpoint}/${encodeURIComponent(id)}`, body, config),
  remove: (id, version, config) => client.delete(`${endpoint}/${encodeURIComponent(id)}`, { ...config, data: { version } })
}
