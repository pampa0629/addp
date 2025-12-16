import { defineStore } from 'pinia'
import { createAuthStore } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('meta-auth',
  createAuthStore('meta-auth', authAPI, {
    persistUser: true
  })
)
