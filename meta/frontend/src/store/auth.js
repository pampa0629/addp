import { defineStore } from 'pinia'
import { createAuthStoreConfig } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('meta-auth', {
  ...createAuthStoreConfig('meta-auth', authAPI, {
    persistUser: true  // Meta 持久化 user
  })
})
