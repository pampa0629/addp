import client from './client'

export const authAPI = {
  login: (username, password) => {
    return client.post('/auth/login', { username, password })
  },

  register: (data) => {
    return client.post('/auth/register', data)
  },

  getMe: () => {
    return client.get('/users/me')
  },

  // createAuthStoreConfig 需要的方法
  getUser: (token) => {
    return client.get('/users/me', {
      headers: { Authorization: `Bearer ${token}` }
    })
  }
}