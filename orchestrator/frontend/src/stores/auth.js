import { defineStore } from 'pinia'
import { createAuthStoreConfig } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('orchestrator-auth', {
  ...createAuthStoreConfig('orchestrator-auth', authAPI, {
    persistUser: true  // Orchestrator 持久化 user
  })
})
