import axios from 'axios'
import { useAuthStore } from '../stores/auth'
import { createRefreshInterceptor } from '@common-ui'

const client = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL || '/api',
  timeout: 300000 // 5分钟超时（用于长SQL查询）
})

// 请求拦截器 - 添加 JWT token 并智能等待用户加载
import { createAuthInterceptor } from '@common-ui'

client.interceptors.request.use(
  createAuthInterceptor(() => useAuthStore(), 'Develop'),
  (error) => Promise.reject(error)
)

// 保存 axios 实例引用 (供刷新后重试使用)
client.interceptors.response.__axiosInstance = client

// 添加 Token 刷新拦截器
const [onFulfilled, onRejected] = createRefreshInterceptor(() => useAuthStore(), {
  moduleName: 'Develop',
  systemBaseURL: 'http://localhost:8080',
  onRefreshFailed: () => {
    localStorage.removeItem('token')
    window.location.href = '/login'
  }
})

client.interceptors.response.use(onFulfilled, onRejected)

export default client
