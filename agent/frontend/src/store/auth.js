import { defineStore } from 'pinia'
import { createAuthStore } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('agent-auth',
  createAuthStore('agent-auth', authAPI, {
    persistUser: true
  })
)
