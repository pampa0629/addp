import { defineStore } from 'pinia'
import { login as loginAPI, getCurrentUser } from '../api/auth'

export const useAuthStore = defineStore('develop-auth', {
  state: () => ({
    token: localStorage.getItem('token') || null,
    user: null
  }),

  getters: {
    isAuthenticated: (state) => !!state.token,
    username: (state) => state.user?.username || ''
  },

  actions: {
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
      try {
        const response = await getCurrentUser()
        this.user = response.data
        return response
      } catch (error) {
        console.error('获取用户信息失败:', error)
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
