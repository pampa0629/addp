import client from './client'

export const enginesAPI = {
  create: (data) => {
    return client.post('/engines', data)
  },

  list: (page = 1, pageSize = 10, engineType = null) => {
    const params = { page, page_size: pageSize }
    if (engineType) params.engine_type = engineType
    return client.get('/engines', { params })
  },

  getById: (id) => {
    return client.get(`/engines/${id}`)
  },

  update: (id, data) => {
    return client.put(`/engines/${id}`, data)
  },

  delete: (id) => {
    return client.delete(`/engines/${id}`)
  },

  testConnection: (data) => {
    return client.post('/engines/test-connection', data)
  },

  testExistingConnection: (id) => {
    return client.post(`/engines/${id}/test`)
  }
}
