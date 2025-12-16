import { defineStore } from 'pinia'
import { createAuthStoreConfig } from '@common-ui'
import { authAPI } from '../api/auth'

const baseConfig = createAuthStoreConfig('develop-auth', authAPI, {
  persistUser: true  // 启用用户信息持久化
})

export const useAuthStore = defineStore('develop-auth', {
  ...baseConfig,

  getters: {
    // 继承基础 getters
    ...baseConfig.getters,
    // 扩展：保留 username getter
    username: (state) => state.user?.username || ''
  }
})
