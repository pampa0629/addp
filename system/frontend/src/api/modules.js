import client from './client'

export const modulesAPI = {
  list: () => client.get('/system/platform/modules'),
  get: (moduleName) => client.get(`/system/platform/modules/${encodeURIComponent(moduleName)}`),
  sources: (params = {}, signal) => client.get('/system/platform/module-log-sources', { params, signal }),
  listInstances: (params = {}) => client.get('/system/platform/module-instances', { params }),
  logs: (moduleName, instanceID, params, signal) => client.get(`/system/platform/modules/${encodeURIComponent(moduleName)}/instances/${encodeURIComponent(instanceID)}/logs`, { params, signal }),
  update: (moduleName, data) => client.put(`/system/platform/modules/${encodeURIComponent(moduleName)}`, data)
}
