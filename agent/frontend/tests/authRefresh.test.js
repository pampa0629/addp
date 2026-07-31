import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, defineStore, setActivePinia } from 'pinia'

import {
  buildLoginRedirectURL,
  clearRuntimeAccessToken,
  createAuthStore,
  createRefreshInterceptor
} from '@common-ui'


describe('shared ADDP token refresh', () => {
  beforeEach(() => {
    const values = new Map()
    vi.stubGlobal('localStorage', {
      getItem: key => values.get(key) || null,
      setItem: (key, value) => values.set(key, String(value)),
      removeItem: key => values.delete(key)
    })
  })

  afterEach(() => {
    clearRuntimeAccessToken()
    vi.unstubAllGlobals()
  })

  it('preserves the current Console path in the login redirect', () => {
    expect(buildLoginRedirectURL({
      pathname: '/system/iam',
      search: '?tab=role-assignments',
      hash: '#current'
    })).toBe('/login?redirect=%2Fsystem%2Fiam%3Ftab%3Drole-assignments%23current')
    expect(buildLoginRedirectURL({ pathname: '/login' })).toBeNull()
  })

  it('retries an Axios request through its owning client after refresh', async () => {
    const authStore = {
      token: 'expired-token',
      refreshAccessToken: vi.fn(async () => 'fresh-token'),
      clearLocalSession: vi.fn()
    }
    const axiosInstance = vi.fn(async config => ({ data: 'ok', config }))
    const onRefreshFailed = vi.fn()
    const [, onRejected] = createRefreshInterceptor(() => authStore, {
      axiosInstance,
      onRefreshFailed
    })
    const config = { url: '/agent/sessions', headers: {} }

    const response = await onRejected({ response: { status: 401 }, config })

    expect(response.data).toBe('ok')
    expect(config._retry).toBe(true)
    expect(config.headers.Authorization).toBe('Bearer fresh-token')
    expect(axiosInstance).toHaveBeenCalledWith(config)
    expect(authStore.refreshAccessToken).toHaveBeenCalledWith({ force: true })
    expect(authStore.clearLocalSession).not.toHaveBeenCalled()
    expect(onRefreshFailed).not.toHaveBeenCalled()
  })

  it('force refreshes once when a peer token is rejected during session initialization', async () => {
    class FakeBroadcastChannel {
      addEventListener(_type, listener) {
        this.listener = listener
      }

      postMessage(message) {
        if (message.type !== 'token-request') return
        queueMicrotask(() => this.listener({
          data: {
            type: 'token',
            sender: 'stale-peer',
            token: 'stale-peer-token',
            expiresAt: Date.now() + 900_000
          }
        }))
      }

      close() {}
    }
    vi.stubGlobal('BroadcastChannel', FakeBroadcastChannel)
    const authenticationFailure = Object.assign(new Error('revoked access token'), {
      response: { status: 401 }
    })
    const authAPI = {
      refresh: vi.fn(async () => ({ data: { access_token: 'refreshed-token', expires_in: 0 } })),
      getUser: vi.fn(async token => {
        if (token === 'stale-peer-token') throw authenticationFailure
        return { data: { id: '4', display_name: 'IAM administrator' } }
      }),
      getAuthContext: vi.fn(async token => {
        if (token === 'stale-peer-token') throw authenticationFailure
        return { data: { context: { type: 'tenant', tenant_id: '1' } } }
      })
    }
    setActivePinia(createPinia())
    const useTestAuthStore = defineStore(
      'auth-bootstrap-recovery',
      createAuthStore('auth-bootstrap-recovery', authAPI, { persistUser: false })
    )
    const store = useTestAuthStore()

    await expect(store.initializeSession()).resolves.toBe('refreshed-token')

    expect(authAPI.refresh).toHaveBeenCalledTimes(1)
    expect(authAPI.getUser).toHaveBeenCalledTimes(2)
    expect(authAPI.getAuthContext).toHaveBeenCalledTimes(2)
    expect(store.token).toBe('refreshed-token')
    expect(store.sessionStatus).toBe('authenticated')
    expect(store.user).toMatchObject({ id: '4' })

    store.sessionInitialized = false
    store.clearLocalSession()
  })
})
