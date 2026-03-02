import { defineStore } from 'pinia'
import { createAuthStore } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('console-auth',
  createAuthStore('console-auth', authAPI, {
    persistUser: true
  })
)
