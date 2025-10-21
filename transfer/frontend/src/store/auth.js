import { defineStore } from 'pinia'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('transfer-auth', {
  state: () => ({
    token: localStorage.getItem('token') || null,
    user: (() => {
      const stored = localStorage.getItem('user')
      if (!stored) {
        return null
      }
      try {
        return JSON.parse(stored)
      } catch {
        return null
      }
    })()
  }),

  getters: {
    isAuthenticated: state => !!state.token
  },

  actions: {
    async login(username, password) {
      const response = await authAPI.login(username, password)
      const accessToken = response.data?.access_token

      if (!accessToken) {
        throw new Error('登录响应缺少访问令牌')
      }

      this.setToken(accessToken)
      await this.fetchUser()
    },

    setToken(token) {
      this.token = token
      if (token) {
        localStorage.setItem('token', token)
      } else {
        localStorage.removeItem('token')
      }
    },

    async fetchUser() {
      if (!this.token) {
        this.user = null
        localStorage.removeItem('user')
        return
      }

      const response = await authAPI.getCurrentUser()
      this.user = response.data
      localStorage.setItem('user', JSON.stringify(this.user))
    },

    logout() {
      this.setToken(null)
      this.user = null
      localStorage.removeItem('user')
    }
  }
})
