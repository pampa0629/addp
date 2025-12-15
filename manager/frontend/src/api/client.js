import axios from 'axios'
import { useAuthStore } from '../store/auth'
import { createRefreshInterceptor } from '@common-ui'

// Manager 服务统一通过 Gateway 访问（开发环境通过 Vite proxy 转发）
const client = axios.create({
  baseURL: '/api',
  timeout: 10000
})

// 请求拦截器 - 添加 JWT token 并智能等待用户加载
import { createAuthInterceptor } from '@common-ui'

client.interceptors.request.use(
  createAuthInterceptor(() => useAuthStore(), 'Manager'),
  error => Promise.reject(error)
)

// 保存 axios 实例引用 (供刷新后重试使用)
client.interceptors.response.__axiosInstance = client

// 添加 Token 刷新拦截器
const [onFulfilled, onRejected] = createRefreshInterceptor(() => useAuthStore(), {
  moduleName: 'Manager',
  systemBaseURL: 'http://localhost:8080',
  onRefreshFailed: () => {
    const authStore = useAuthStore()
    authStore.logout()
    window.location.href = '/login'
  }
})

client.interceptors.response.use(onFulfilled, onRejected)

export default client