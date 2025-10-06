import axios from 'axios'

// 创建 System API 客户端 (直接访问 System 模块)
const systemClient = axios.create({
  baseURL: 'http://localhost:8080',
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
  // 获取资源列表
  getResources(params) {
    return systemClient.get('/api/resources', { params })
  },

  // 获取单个资源
  getResource(id) {
    return systemClient.get(`/api/resources/${id}`)
  }
}
