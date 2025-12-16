import { defineStore } from 'pinia'
import { createAuthStore } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('develop-auth',
  createAuthStore('develop-auth', authAPI, {
    persistUser: true,
    extraGetters: {
      username: (state) => state.user?.username || ''
    }
  })
)
