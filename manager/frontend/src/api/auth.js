import axios from 'axios'

// 认证请求直接访问 System 服务（通过 Gateway）
const authClient = axios.create({
  baseURL: import.meta.env.PROD ? '/api' : 'http://localhost:8080/api',
  timeout: 10000
})

export const authAPI = {
  login(username, password) {
    // 适配 createAuthStoreConfig 的 login(username, password) 调用方式
    return authClient.post('/auth/login', { username, password })
  },

  register(userData) {
    return authClient.post('/auth/register', userData)
  },

  getUser(token) {
    // createAuthStoreConfig 需要的方法名是 getUser 而不是 getCurrentUser
    return authClient.get('/users/me', {
      headers: { Authorization: `Bearer ${token}` }
    })
  }
}