import { defineStore } from 'pinia'
import { createAuthStore } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('inference-auth',
  createAuthStore('inference-auth', authAPI, { persistUser: true })
)
