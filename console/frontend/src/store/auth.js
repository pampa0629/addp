import { defineStore } from 'pinia'
import { createAuthStore } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('console-auth',
  createAuthStore('console-auth', authAPI, {
    persistUser: true,
    extraActions: {
      async acceptIssuedSession(session) {
        if (!session?.access_token) throw new Error('auth_invitation_session_missing_access_token')
        this.setToken(session.access_token, session.expires_in)
        await this.fetchSessionState()
        this.sessionInitialized = true
        this.sessionStatus = 'authenticated'
        return session
      }
    }
  })
)
