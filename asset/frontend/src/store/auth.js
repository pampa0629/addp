import { defineStore } from 'pinia'
import { createAuthStore } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('asset-auth',
  createAuthStore('asset-auth', authAPI, {
    persistUser: true
  })
)
