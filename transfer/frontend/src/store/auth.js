import { defineStore } from 'pinia'
import { createAuthStore } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('transfer-auth',
  createAuthStore('transfer-auth', authAPI, {
    persistUser: true
  })
)
