import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, defineStore, setActivePinia } from 'pinia'
import {
  clearRuntimeAccessToken,
  createAuthStore,
  createRefreshInterceptor,
  setRuntimeAccessToken
} from '@common-ui'

describe('Standard authorization refresh', () => {
  beforeEach(() => {
    const values = new Map()
    vi.stubGlobal('localStorage', {
      getItem: key => values.get(key) || null,
      setItem: (key, value) => values.set(key, String(value)),
      removeItem: key => values.delete(key)
    })
    setActivePinia(createPinia())
  })

  afterEach(() => {
    clearRuntimeAccessToken()
    vi.unstubAllGlobals()
  })

  it('reloads permissions after the runtime token changes', async () => {
    const authAPI = {
      refresh: vi.fn(),
      logout: vi.fn(),
      getUser: vi.fn(async token => ({ data: { id: token } })),
      getAuthContext: vi.fn(async token => ({
        data: {
          context: { type: 'tenant' },
          authorization: {
            role_assignments: [{ permissions: [token === 'new-token' ? 'standard.glossary.create' : 'standard.glossary.read'] }]
          }
        }
      }))
    }
    const useTestAuthStore = defineStore('standard-auth-refresh', createAuthStore('standard-auth-refresh', authAPI, { persistUser: false }))
    const store = useTestAuthStore()

    setRuntimeAccessToken('old-token')
    await store.initializeSession()
    expect(store.hasPermission('standard.glossary.read')).toBe(true)

    setRuntimeAccessToken('new-token')
    await vi.waitFor(() => expect(store.hasPermission('standard.glossary.create')).toBe(true))
    expect(store.hasPermission('standard.glossary.read')).toBe(false)
    expect(authAPI.getAuthContext).toHaveBeenCalledTimes(2)
  })

  it('does not let a stale authorization response overwrite the current token', async () => {
    let resolveOld
    const oldContext = new Promise(resolve => { resolveOld = resolve })
    const authAPI = {
      refresh: vi.fn(),
      logout: vi.fn(),
      getUser: vi.fn(async token => ({ data: { id: token } })),
      getAuthContext: vi.fn(token => token === 'old-token'
        ? oldContext
        : Promise.resolve({ data: { authorization: { role_assignments: [{ permissions: ['standard.glossary.create'] }] } } }))
    }
    const useTestAuthStore = defineStore('standard-auth-race', createAuthStore('standard-auth-race', authAPI, { persistUser: false }))
    const store = useTestAuthStore()

    setRuntimeAccessToken('old-token')
    const initialized = store.initializeSession()
    setRuntimeAccessToken('new-token')
    resolveOld({ data: { authorization: { role_assignments: [{ permissions: ['standard.glossary.delete'] }] } } })
    await initialized
    await vi.waitFor(() => expect(store.hasPermission('standard.glossary.create')).toBe(true))
    expect(store.hasPermission('standard.glossary.delete')).toBe(false)
  })

  it('ignores a stale authorization failure after a newer token is active', async () => {
    let rejectOld
    const oldContext = new Promise((_resolve, reject) => { rejectOld = reject })
    const authAPI = {
      refresh: vi.fn(),
      logout: vi.fn(),
      getUser: vi.fn(async token => ({ data: { id: token } })),
      getAuthContext: vi.fn(token => token === 'old-token'
        ? oldContext
        : Promise.resolve({ data: { authorization: { role_assignments: [{ permissions: ['standard.glossary.create'] }] } } }))
    }
    const useTestAuthStore = defineStore('standard-auth-stale-failure', createAuthStore('standard-auth-stale-failure', authAPI, { persistUser: false }))
    const store = useTestAuthStore()

    store.bindAuthSession()
    store.sessionInitialized = true
    store.sessionStatus = 'authenticated'
    setRuntimeAccessToken('old-token')
    await vi.waitFor(() => expect(authAPI.getAuthContext).toHaveBeenCalledWith('old-token'))
    setRuntimeAccessToken('new-token')
    rejectOld(Object.assign(new Error('old token rejected'), { response: { status: 401 } }))
    await vi.waitFor(() => expect(store.hasPermission('standard.glossary.create')).toBe(true))
    expect(store.sessionStatus).toBe('authenticated')
  })

  it('does not refresh or retry a forbidden request', async () => {
    const authStore = {
      refreshAccessToken: vi.fn(),
      clearLocalSession: vi.fn()
    }
    const axiosInstance = vi.fn()
    const [, onRejected] = createRefreshInterceptor(() => authStore, { axiosInstance })
    const error = {
      response: {
        status: 403,
        data: { error: '无权审批该术语', error_code: 'permission_denied' }
      },
      config: { method: 'post', url: '/standard/glossaries/1/approve', headers: {} }
    }

    await expect(onRejected(error)).rejects.toBe(error)
    expect(authStore.refreshAccessToken).not.toHaveBeenCalled()
    expect(authStore.clearLocalSession).not.toHaveBeenCalled()
    expect(axiosInstance).not.toHaveBeenCalled()
  })
})
