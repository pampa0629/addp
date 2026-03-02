import { defineStore } from 'pinia'
import { createAuthStore } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('service-auth',
  createAuthStore('service-auth', authAPI, {
    persistUser: true
  })
)
