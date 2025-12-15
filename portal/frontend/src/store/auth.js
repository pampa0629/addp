import { defineStore } from 'pinia'
import { createAuthStoreConfig } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('portal-auth', {
  ...createAuthStoreConfig('portal-auth', authAPI, {
    persistUser: true
  })
})
