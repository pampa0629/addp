import axios from 'axios'
import { useAuthStore } from '../store/auth'
import { createRefreshInterceptor } from '@common-ui'

// Meta 服务统一通过 Gateway 访问（开发环境通过 Vite proxy 转发）
const client = axios.create({
  baseURL: '/api',
  timeout: 30000
})

// 请求拦截器 - 添加 JWT token 并智能等待用户加载
import { createAuthInterceptor } from '@common-ui'

client.interceptors.request.use(
  createAuthInterceptor(() => useAuthStore(), 'Meta'),
  error => Promise.reject(error)
)

// 保存 axios 实例引用 (供刷新后重试使用)
client.interceptors.response.__axiosInstance = client

// 添加 Token 刷新拦截器（同时处理 response.data 提取）
const [refreshOnFulfilled, refreshOnRejected] = createRefreshInterceptor(() => useAuthStore(), {
  moduleName: 'Meta',
  systemBaseURL: 'http://localhost:8080',
  onRefreshFailed: () => {
    console.error('=== Token 刷新失败，清除token并跳转到登录页 ===')
    localStorage.removeItem('token')
    window.location.href = '/meta/login'
  }
})

// 响应拦截器 - 先刷新 Token，再提取 response.data
client.interceptors.response.use(
  response => {
    // 先通过刷新拦截器处理
    const processedResponse = refreshOnFulfilled(response)
    // 然后提取 data（保持原有行为）
    return processedResponse.data
  },
  async error => {
    try {
      // 先尝试通过刷新拦截器处理（可能刷新 token 并重试）
      return await refreshOnRejected(error)
    } catch (finalError) {
      // 如果不是 401 错误，记录详细信息
      if (finalError.response?.status === 401) {
        console.error('=== 401 未授权错误 ===')
        console.error('请求URL:', finalError.config?.url)
        console.error('请求方法:', finalError.config?.method)
        console.error('响应数据:', finalError.response?.data)
      }
      return Promise.reject(finalError)
    }
  }
)

export default client
