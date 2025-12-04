import { defineStore } from 'pinia'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: localStorage.getItem('token') || null,
    user: null
  }),

  getters: {
    isAuthenticated: (state) => !!state.token
  },

  actions: {
    async login(username, password) {
      try {
        const response = await authAPI.login(username, password)
        this.token = response.data.access_token
        localStorage.setItem('token', this.token)
        await this.fetchUser()
      } catch (error) {
        console.error('Auth Store - 登录失败:', error)
        throw error  // 重新抛出错误让调用者处理
      }
    },

    setToken(token) {
      this.token = token
      localStorage.setItem('token', token)
    },

    async fetchUser() {
      try {
        const response = await authAPI.getMe()
        this.user = response.data
      } catch (error) {
        console.error('Auth Store - 获取用户信息失败:', error)
        throw error
      }
    },

    logout() {
      this.token = null
      this.user = null
      localStorage.removeItem('token')
    }
  }
})