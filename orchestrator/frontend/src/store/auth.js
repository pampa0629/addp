import { defineStore } from 'pinia'
import { createAuthStore } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('orchestrator-auth',
  createAuthStore('orchestrator-auth', authAPI, {
    persistUser: true
  })
)
