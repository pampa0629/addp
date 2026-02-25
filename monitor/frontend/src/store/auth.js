import { defineStore } from 'pinia'
import { createAuthStore } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('monitor-auth',
  createAuthStore('monitor-auth', authAPI, {
    persistUser: true
  })
)
