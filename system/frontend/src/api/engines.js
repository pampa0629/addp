import client from './client'

export const enginesAPI = {
  create: (data) => {
    return client.post('/system/engines', data)
  },

  list: (page = 1, pageSize = 10, engineType = null) => {
    const params = { page, page_size: pageSize }
    if (engineType) params.engine_type = engineType
    return client.get('/system/engines', { params })
  },

  getById: (id) => {
    return client.get(`/system/engines/${id}`)
  },

  update: (id, data) => {
    return client.put(`/system/engines/${id}`, data)
  },

  delete: (id) => {
    return client.delete(`/system/engines/${id}`)
  },

  testConnection: (data) => {
    return client.post('/system/engines/test-connection', data)
  },

  testConnectionBeforeCreate: (data) => {
    return client.post('/system/engines/test-connection', data)
  },

  testExistingConnection: (id, data = null) => {
    if (data) {
      return client.post(`/system/engines/${id}/test`, data)
    }
    return client.post(`/system/engines/${id}/test`)
  }
}
