import { defineStore } from 'pinia'
import { createAuthStore } from '@common-ui'
import { authAPI } from '../api/auth'
export const useAuthStore = defineStore('workbench-auth', createAuthStore('workbench-auth', authAPI, { persistUser: true }))
