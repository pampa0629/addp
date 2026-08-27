import { defineStore } from 'pinia'
import { createAuthStore } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('catalog-auth',
  createAuthStore('catalog-auth', authAPI, { persistUser: true })
)
