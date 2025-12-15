import axios from 'axios'

const authClient = axios.create({
  baseURL: import.meta.env.DEV ? 'http://localhost:8080/api' : '/api',
  timeout: 15000
})

authClient.interceptors.request.use(
  config => {
    const token = localStorage.getItem('token')
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    return config
  },
  error => Promise.reject(error)
)

authClient.interceptors.response.use(
  response => response,
  error => {
    if (error.response?.status === 401) {
      localStorage.removeItem('token')
      window.location.href = '/meta/login'
    }
    return Promise.reject(error)
  }
)

export const authAPI = {
  login: (username, password) => {
    return authClient.post('/auth/login', { username, password })
  },

  getCurrentUser: () => {
    return authClient.get('/users/me')
  },

  // createAuthStoreConfig 需要的方法
  getUser: (token) => {
    return authClient.get('/users/me', {
      headers: { Authorization: `Bearer ${token}` }
    })
  }
}
