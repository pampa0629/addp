import { defineStore } from 'pinia'
import { createAuthStore } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('graph-auth',
  createAuthStore('graph-auth', authAPI, {
    persistUser: true
  })
)
