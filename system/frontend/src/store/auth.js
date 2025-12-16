import { defineStore } from 'pinia'
import { createAuthStore } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('system-auth',
  createAuthStore('system-auth', authAPI, {
    persistUser: true  // 持久化用户信息，刷新后快速恢复
  })
)
