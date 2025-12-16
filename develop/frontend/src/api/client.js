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
  systemBaseURL: import.meta.env.DEV
    ? 'http://localhost:8080'
    : `${window.location.protocol}//${window.location.hostname}:8080`,
  onRefreshFailed: () => {
    localStorage.removeItem('token')
    window.location.href = '/login'
  }
})

// 响应拦截器：提取 response.data 并增强错误日志
client.interceptors.response.use(
  response => {
    const processedResponse = onFulfilled(response)
    return processedResponse.data  // 提取 data
  },
  async error => {
    try {
      const recovered = await onRejected(error)
      return recovered.data
    } catch (finalError) {
      // 详细的 401 错误日志
      if (finalError.response?.status === 401) {
        console.error('[Develop Client] 401 错误详情:', {
          url: finalError.config?.url,
          method: finalError.config?.method,
          response: finalError.response?.data
        })
      }
      return Promise.reject(finalError)
    }
  }
)

export default client
