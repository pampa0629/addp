import axios from 'axios'

const authClient = axios.create({
  baseURL: 'http://localhost:8080/api', // System 服务
  timeout: 10000
})

/**
 * 用户登录
 */
export const login = (username, password) => {
  return authClient.post('/auth/login', { username, password })
}

/**
 * 获取当前用户信息
 */
export const getCurrentUser = () => {
  const token = localStorage.getItem('token')
  return authClient.get('/users/me', {
    headers: { Authorization: `Bearer ${token}` }
  })
}
