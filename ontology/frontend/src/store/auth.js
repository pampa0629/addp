import { defineStore } from 'pinia'
import { createAuthStore } from '@common-ui'
import { authAPI } from '../api/auth'
export const useAuthStore = defineStore(
  'ontology-auth',
  createAuthStore('ontology-auth', authAPI, { persistUser: true })
)
