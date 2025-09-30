import client from './client'

export const resourcesAPI = {
  create: (data) => {
    return client.post('/resources', data)
  },

  list: (page = 1, pageSize = 10, resourceType = null) => {
    const params = { page, page_size: pageSize }
    if (resourceType) params.resource_type = resourceType
    return client.get('/resources', { params })
  },

  getById: (id) => {
    return client.get(`/resources/${id}`)
  },

  update: (id, data) => {
    return client.put(`/resources/${id}`, data)
  },

  delete: (id) => {
    return client.delete(`/resources/${id}`)
  },

  testConnection: (data) => {
    return client.post('/resources/test-connection', data)
  },

  testExistingConnection: (id) => {
    return client.post(`/resources/${id}/test`)
  }
}