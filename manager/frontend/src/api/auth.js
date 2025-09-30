import axios from 'axios'

// 认证请求直接访问 System 服务（通过 Gateway）
const authClient = axios.create({
  baseURL: import.meta.env.PROD ? '/api' : 'http://localhost:8080/api',
  timeout: 10000
})

export const authAPI = {
  login(credentials) {
    return authClient.post('/auth/login', credentials)
  },

  register(userData) {
    return authClient.post('/auth/register', userData)
  },

  getCurrentUser(token) {
    return authClient.get('/users/me', {
      headers: { Authorization: `Bearer ${token}` }
    })
  }
}