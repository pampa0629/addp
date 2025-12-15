import { defineStore } from 'pinia'
import { createAuthStoreConfig } from '@common-ui'
import { authAPI as systemAuthAPI } from '../api/auth'

// 适配器：System 使用 getMe() 而非 getCurrentUser()
const authAPI = {
  login: systemAuthAPI.login,
  getUser: () => systemAuthAPI.getMe()  // 适配方法名
}

export const useAuthStore = defineStore('system-auth', {
  ...createAuthStoreConfig('system-auth', authAPI, {
    persistUser: false  // System 不持久化 user
  })
})