import axios from 'axios'

// 创建 System API 客户端
// 开发环境: 直接访问 System backend (localhost:8080)
// 生产环境: 通过独立端口访问 System backend (host:8080)
const systemClient = axios.create({
  baseURL: import.meta.env.DEV
    ? 'http://localhost:8080'
    : `${window.location.protocol}//${window.location.hostname}:8080`,
  timeout: 30000
})

// 请求拦截器
systemClient.interceptors.request.use(
  (config) => {
    const token = localStorage.getItem('token')
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    return config
  },
  (error) => {
    return Promise.reject(error)
  }
)

// 响应拦截器
systemClient.interceptors.response.use(
  (response) => response.data,
  (error) => {
    if (error.response?.status === 401) {
      // Token过期,跳转登录
      localStorage.removeItem('token')
      window.location.href = '/meta/login'
    }
    return Promise.reject(error)
  }
)

export default {
  // 获取引擎列表
  getEngines(params) {
    return systemClient.get('/api/engines', { params })
  },

  // 获取单个引擎
  getEngine(id) {
    return systemClient.get(`/api/engines/${id}`)
  }
}
