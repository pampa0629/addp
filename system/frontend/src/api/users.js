import client from './client'

export const usersAPI = {
  create: (data) => {
    return client.post('/system/users', data)
  },

  list: (page = 1, pageSize = 10) => {
    return client.get('/system/users', { params: { page, page_size: pageSize } })
  },

  getById: (id) => {
    return client.get(`/system/users/${id}`)
  },

  update: (id, data) => {
    return client.put(`/system/users/${id}`, data)
  },

  delete: (id) => {
    return client.delete(`/system/users/${id}`)
  },

  me: () => {
    return client.get('/system/users/me')
  },

  changePassword: (id, data) => {
    return client.put(`/system/users/${id}/change-password`, data)
  }
}