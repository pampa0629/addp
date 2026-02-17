import { defineStore } from 'pinia'
import { createAuthStore } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('portal-auth',
  createAuthStore('portal-auth', authAPI, {
    persistUser: true
  })
)
