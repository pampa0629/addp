import client from './client'

export const modulesAPI = {
  list: () => client.get('/system/platform/modules'),
  get: (moduleName) => client.get(`/system/platform/modules/${encodeURIComponent(moduleName)}`),
  listInstances: (moduleName, params = {}) => client.get(
    `/system/platform/modules/${encodeURIComponent(moduleName)}/instances`,
    { params }
  ),
  update: (moduleName, data) => client.put(`/system/platform/modules/${encodeURIComponent(moduleName)}`, data)
}
