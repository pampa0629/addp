import axios from 'axios'
import { useAuthStore } from '../store/auth'
import { createAuthInterceptor, createRefreshInterceptor } from '@common-ui'

const client = axios.create({
  baseURL: import.meta.env.PROD ? '/api' : 'http://localhost:8080/api',
  timeout: 10000
})

// 请求拦截器 - 使用统一的 createAuthInterceptor
client.interceptors.request.use(
  createAuthInterceptor(() => useAuthStore(), 'Portal'),
  error => Promise.reject(error)
)

// 保存 axios 实例引用 (供刷新后重试使用)
client.interceptors.response.__axiosInstance = client

// 响应拦截器 - 使用自动刷新 Token 拦截器
const [onFulfilled, onRejected] = createRefreshInterceptor(() => useAuthStore(), {
  moduleName: 'Portal',
  systemBaseURL: import.meta.env.PROD ? '' : 'http://localhost:8080',
  onRefreshFailed: () => {
    const authStore = useAuthStore()
    authStore.logout()
    window.location.href = '/login'
  }
})

client.interceptors.response.use(onFulfilled, onRejected)

export default client