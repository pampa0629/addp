import axios from 'axios'

const client = axios.create({
  baseURL: '/api',
  timeout: 30000
})

// 请求拦截器
client.interceptors.request.use(
  config => {
    // TODO: 添加认证 token
    return config
  },
  error => Promise.reject(error)
)

// 响应拦截器
client.interceptors.response.use(
  response => response.data,
  error => {
    console.error('API Error:', error)
    return Promise.reject(error)
  }
)

export default client
