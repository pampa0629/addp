import axios from 'axios'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../stores/auth'
import { createRefreshInterceptor } from '@common-ui'

const client = axios.create({
  baseURL: '/api',
  timeout: 30000
})

// 请求拦截器 - 添加 JWT token 并智能等待用户加载
import { createAuthInterceptor } from '@common-ui'

client.interceptors.request.use(
  createAuthInterceptor(() => useAuthStore(), 'Orchestrator'),
  error => Promise.reject(error)
)

// 保存 axios 实例引用 (供刷新后重试使用)
client.interceptors.response.__axiosInstance = client

// 添加 Token 刷新拦截器（同时保留原有错误提示）
const [refreshOnFulfilled, refreshOnRejected] = createRefreshInterceptor(() => useAuthStore(), {
  moduleName: 'Orchestrator',
  systemBaseURL: 'http://localhost:8080',
  onRefreshFailed: () => {
    ElMessage.error('未授权，请重新登录')
    localStorage.removeItem('token')
    window.location.href = '/login'
  }
})

// 响应拦截器 - 先刷新 Token，再处理业务逻辑
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
      // Token 刷新失败或其他错误，显示错误提示
      if (finalError.response) {
        const { status, data } = finalError.response

        if (status === 401) {
          // 401 已在 onRefreshFailed 中处理
        } else if (status === 403) {
          ElMessage.error('没有权限访问')
        } else if (status >= 500) {
          ElMessage.error(data.error || data.message || '服务器错误')
        } else {
          ElMessage.error(data.error || data.message || '请求失败')
        }
      } else if (finalError.request) {
        ElMessage.error('网络错误，请检查网络连接')
      } else {
        ElMessage.error('请求配置错误')
      }

      return Promise.reject(finalError)
    }
  }
)

export default client
