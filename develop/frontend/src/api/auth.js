import axios from 'axios'

const authClient = axios.create({
  baseURL: import.meta.env.DEV
    ? 'http://localhost:8080/api'
    : '/api',
  timeout: 15000
})

/**
 * 认证 API
 */
export const authAPI = {
  /**
   * 用户登录
   */
  login(username, password) {
    return authClient.post('/auth/login', { username, password })
  },

  /**
   * 获取当前用户信息
   */
  getCurrentUser() {
    const token = localStorage.getItem('token')
    return authClient.get('/users/me', {
      headers: { Authorization: `Bearer ${token}` }
    })
  },

  /**
   * 获取用户信息（供 store 使用）
   */
  getUser() {
    return this.getCurrentUser()
  }
}
