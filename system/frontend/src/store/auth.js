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
      const response = await authAPI.login(username, password)
      this.token = response.data.access_token
      localStorage.setItem('token', this.token)
      await this.fetchUser()
    },

    setToken(token) {
      this.token = token
      localStorage.setItem('token', token)
    },

    async fetchUser() {
      const response = await authAPI.getMe()
      this.user = response.data
    },

    logout() {
      this.token = null
      this.user = null
      localStorage.removeItem('token')
    }
  }
})