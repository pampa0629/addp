import { defineStore } from 'pinia'
import { createAuthStore } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('standard-auth',
  createAuthStore('standard-auth', authAPI, {
    persistUser: true
  })
)
