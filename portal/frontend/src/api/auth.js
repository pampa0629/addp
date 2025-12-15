import client from './client'

export const authAPI = {
  login(username, password) {
    // 适配 createAuthStoreConfig 的 login(username, password) 调用方式
    return client.post('/auth/login', { username, password })
  },

  register(userData) {
    return client.post('/auth/register', userData)
  },

  getMe() {
    return client.get('/users/me')
  },

  // 适配器方法 (createAuthStoreConfig 需要)
  getUser(token) {
    return client.get('/users/me', {
      headers: { Authorization: `Bearer ${token}` }
    })
  }
}