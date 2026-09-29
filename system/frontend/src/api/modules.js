import client from './client'

export const modulesAPI = {
  list: () => client.get('/system/platform/modules'),
  get: (moduleName) => client.get(`/system/platform/modules/${encodeURIComponent(moduleName)}`),
  listInstances: (params = {}) => client.get('/system/platform/module-instances', { params }),
  update: (moduleName, data) => client.put(`/system/platform/modules/${encodeURIComponent(moduleName)}`, data)
}
