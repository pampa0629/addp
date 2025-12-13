import { defineStore } from 'pinia'
import { login as loginAPI, getCurrentUser } from '../api/auth'

export const useAuthStore = defineStore('develop-auth', {
  state: () => ({
    token: localStorage.getItem('token') || null,
    user: null,
    isLoadingUser: false  // 新增：用户信息加载状态标志位
  }),

  getters: {
    isAuthenticated: (state) => !!state.token,
    username: (state) => state.user?.username || ''
  },

  actions: {
    // 设置 token（供路由守卫调用）
    setToken(token) {
      this.token = token
      localStorage.setItem('token', token)
    },

    async login(username, password) {
      try {
        const response = await loginAPI(username, password)
        this.token = response.data.access_token
        localStorage.setItem('token', this.token)
        await this.fetchUser()
        return response
      } catch (error) {
        throw error
      }
    },

    async fetchUser() {
      // 防止重复请求：如果已经在加载中，等待当前请求完成
      if (this.isLoadingUser) {
        console.log('[AuthStore] fetchUser already in progress, waiting...')
        // 等待当前请求完成（通过轮询检查）
        return new Promise((resolve, reject) => {
          const checkInterval = setInterval(() => {
            if (!this.isLoadingUser) {
              clearInterval(checkInterval)
              if (this.user) {
                resolve({ data: this.user })
              } else {
                reject(new Error('User fetch failed'))
              }
            }
          }, 50)  // 每50ms检查一次

          // 10秒超时保护，避免永久卡死
          setTimeout(() => {
            clearInterval(checkInterval)
            reject(new Error('fetchUser timeout'))
          }, 10000)
        })
      }

      this.isLoadingUser = true  // 标记开始加载

      try {
        const response = await getCurrentUser()
        this.user = response.data
        return response
      } catch (error) {
        console.error('获取用户信息失败:', error)
        throw error
      } finally {
        this.isLoadingUser = false  // 标记加载完成（无论成功或失败）
      }
    },

    logout() {
      this.token = null
      this.user = null
      this.isLoadingUser = false  // 重置加载状态
      localStorage.removeItem('token')
    }
  }
})
