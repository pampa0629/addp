import { defineStore } from 'pinia'
import { createAuthStoreConfig } from '@common-ui'
import { login as loginAPI, getCurrentUser } from '../api/auth'

// 适配器：Develop 使用独立函数导出
const authAPI = {
  login: (username, password) => loginAPI(username, password),
  getUser: () => getCurrentUser()
}

export const useAuthStore = defineStore('develop-auth', {
  ...createAuthStoreConfig('develop-auth', authAPI, {
    persistUser: false  // Develop 不持久化 user
  }),

  getters: {
    // 扩展：保留 username getter
    username: (state) => state.user?.username || ''
  }
})
