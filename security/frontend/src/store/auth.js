import { defineStore } from 'pinia'
import { createAuthStore } from '@common-ui'
import { authAPI } from '../api/auth'
export const useAuthStore = defineStore('security-auth', createAuthStore('security-auth', authAPI, { persistUser: true }))
