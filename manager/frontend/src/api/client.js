import axios from 'axios'
import { useAuthStore } from '../store/auth'

// Manager 服务通过 Gateway 访问
const client = axios.create({
  baseURL: import.meta.env.PROD ? '/api' : 'http://localhost:8081/api',
  timeout: 10000
})

client.interceptors.request.use(
  config => {
    const authStore = useAuthStore()
    if (authStore.token) {
      config.headers.Authorization = `Bearer ${authStore.token}`
    }
    return config
  },
  error => {
    return Promise.reject(error)
  }
)

client.interceptors.response.use(
  response => response,
  error => {
    if (error.response?.status === 401) {
      const authStore = useAuthStore()
      authStore.logout()
      window.location.href = '/login'
    }
    return Promise.reject(error)
  }
)

export default client