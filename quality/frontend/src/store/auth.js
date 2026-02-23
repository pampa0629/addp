import { defineStore } from 'pinia'
import { createAuthStore } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('quality-auth',
  createAuthStore('quality-auth', authAPI, {
    persistUser: true
  })
)
