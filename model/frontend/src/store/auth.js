import { defineStore } from 'pinia'
import { createAuthStore } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('model-auth',
  createAuthStore('model-auth', authAPI, {
    persistUser: true
  })
)
