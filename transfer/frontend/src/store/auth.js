import { defineStore } from 'pinia'
import { createAuthStoreConfig } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('transfer-auth', {
  ...createAuthStoreConfig('transfer-auth', authAPI, {
    persistUser: true  // Transfer 持久化 user
  })
})
