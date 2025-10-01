import client from './client'

export const usersAPI = {
  create: (data) => {
    return client.post('/users', data)
  },

  list: (page = 1, pageSize = 10) => {
    return client.get('/users', { params: { page, page_size: pageSize } })
  },

  getById: (id) => {
    return client.get(`/users/${id}`)
  },

  update: (id, data) => {
    return client.put(`/users/${id}`, data)
  },

  delete: (id) => {
    return client.delete(`/users/${id}`)
  },

  me: () => {
    return client.get('/users/me')
  }
}