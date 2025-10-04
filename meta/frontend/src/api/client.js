import axios from 'axios'

const client = axios.create({
  baseURL: import.meta.env.DEV ? 'http://localhost:8082' : '',
  timeout: 30000
})

// 请求拦截器 - 添加 token
client.interceptors.request.use(
  config => {
    const token = localStorage.getItem('token')
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    return config
  },
  error => Promise.reject(error)
)

// 响应拦截器 - 处理错误
client.interceptors.response.use(
  response => response.data,
  error => {
    if (error.response?.status === 401) {
      console.error('=== 401 未授权错误 ===')
      console.error('请求URL:', error.config?.url)
      console.error('请求方法:', error.config?.method)
      console.error('响应数据:', error.response?.data)
      console.error('即将清除token并跳转到登录页')

      localStorage.removeItem('token')
      window.location.href = '/meta/login'
    }
    return Promise.reject(error)
  }
)

export default client
