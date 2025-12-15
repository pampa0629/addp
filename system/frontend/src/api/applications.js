import client from './client'

export const applicationsAPI = {
  // 应用管理
  create: (data) => {
    return client.post('/applications', data)
  },

  list: () => {
    return client.get('/applications')
  },

  getById: (id) => {
    return client.get(`/applications/${id}`)
  },

  update: (id, data) => {
    return client.put(`/applications/${id}`, data)
  },

  delete: (id) => {
    return client.delete(`/applications/${id}`)
  },

  // API Key 管理
  generateKey: (appId, data) => {
    return client.post(`/applications/${appId}/keys`, data)
  },

  listKeys: (appId) => {
    return client.get(`/applications/${appId}/keys`)
  },

  revokeKey: (appId, keyId) => {
    return client.delete(`/applications/${appId}/keys/${keyId}`)
  }
}
