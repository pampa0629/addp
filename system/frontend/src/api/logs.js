import client from './client'

export const logsAPI = {
  list: (page = 1, pageSize = 20, userId = null) => {
    const params = { page, page_size: pageSize }
    if (userId) params.user_id = userId
    return client.get('/logs', { params })
  },

  getById: (id) => {
    return client.get(`/logs/${id}`)
  }
}