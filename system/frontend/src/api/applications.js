import client from './client'

export const applicationsAPI = {
  // 应用管理
  create: (data) => {
    return client.post('/system/applications', data)
  },

  list: () => {
    return client.get('/system/applications')
  },

  getById: (id) => {
    return client.get(`/system/applications/${id}`)
  },

  update: (id, data) => {
    return client.put(`/system/applications/${id}`, data)
  },

  delete: (id) => {
    return client.delete(`/system/applications/${id}`)
  },

  // API Key 管理
  generateKey: (appId, data) => {
    return client.post(`/system/applications/${appId}/keys`, data)
  },

  listKeys: (appId) => {
    return client.get(`/system/applications/${appId}/keys`)
  },

  revokeKey: (appId, keyId) => {
    return client.delete(`/system/applications/${appId}/keys/${keyId}`)
  }
}
