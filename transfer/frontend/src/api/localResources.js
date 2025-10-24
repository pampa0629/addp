import client from './client'

export const localResourcesAPI = {
  list: (resourceType = null) => {
    const params = {}
    if (resourceType) {
      params.resource_type = resourceType
    }
    return client.get('/local-resources', { params })
  },
  create: (data) => client.post('/local-resources', data),
  update: (id, data) => client.put(`/local-resources/${id}`, data),
  delete: (id) => client.delete(`/local-resources/${id}`),
  testConnection: (data) => client.post('/local-resources/test-connection', data),
  testExisting: (id) => client.post(`/local-resources/${id}/test`),
  syncToSystem: (id) => client.post(`/local-resources/${id}/sync`)
}
