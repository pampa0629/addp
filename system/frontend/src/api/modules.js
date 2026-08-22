import client from './client'

export const modulesAPI = {
  list: () => client.get('/system/platform/modules'),
  get: (moduleName) => client.get(`/system/platform/modules/${encodeURIComponent(moduleName)}`),
  update: (moduleName, data) => client.put(`/system/platform/modules/${encodeURIComponent(moduleName)}`, data)
}
