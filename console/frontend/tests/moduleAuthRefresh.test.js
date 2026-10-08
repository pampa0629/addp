import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, defineStore, setActivePinia } from 'pinia'

describe('module guard during authoritative authorization refresh', () => {
  let store, api, transport, guard, context
  const target = { path: '/engines/2', fullPath: '/engines/2?tab=data-authorization', name: 'EngineDetail', matched: [{ meta: { requiresAuth: true } }] }
  const deferred = () => {
    let resolve, reject
    const promise = new Promise((yes, no) => { resolve = yes; reject = no })
    return { promise, resolve, reject }
  }
  const rotate = (token = 'new-token') => {
    const request = deferred()
    api.getAuthContext.mockImplementationOnce(() => request.promise)
    store.setToken(token)
    return request
  }

  beforeEach(async () => {
    vi.resetModules()
    setActivePinia(createPinia())
    const browserWindow = new EventTarget()
    browserWindow.self = browserWindow
    browserWindow.top = browserWindow
    browserWindow.location = { pathname: '/', assign: vi.fn() }
    vi.stubGlobal('window', browserWindow)
    vi.stubGlobal('document', new EventTarget())
    vi.stubGlobal('localStorage', { removeItem: vi.fn() })
    vi.stubGlobal('BroadcastChannel', undefined)
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    context = { context: { type: 'tenant', tenant_id: '2' }, authorization: { role_assignments: [
      { scope: { type: 'tenant', tenant_id: '2' }, permissions: ['system.engine.read'], valid_until: null }
    ] } }
    api = { getAuthContext: vi.fn(async () => context) }
    const auth = await import('../../../common-frontend/basic/src/composables/useAuth.js')
    transport = await import('../../../common-frontend/basic/src/auth/authSession.js')
    store = defineStore('guard-refresh-auth', auth.createAuthStore('guard-refresh-auth', api, { persistUser: false }))()
    store.setToken('old-token')
    await store.fetchAuthContext()
    store.sessionInitialized = true
    store.sessionStatus = 'authenticated'
    guard = auth.createAuthGuard(store, { moduleName: 'System', router: { hasRoute: () => true } })
  })

  afterEach(() => {
    store?.$dispose()
    transport?.clearRuntimeAccessToken()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('keeps navigation pending with empty candidate permissions until the authority responds', async () => {
    const request = rotate(), next = vi.fn()
    const navigation = guard(target, null, next)
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(store.permissions).toEqual([])
    expect(next).not.toHaveBeenCalled()
    request.resolve(context)
    await navigation
    expect(next).toHaveBeenCalledWith()
  })

  it('denies only after the refreshed authority actually revokes engine access', async () => {
    const request = rotate(), next = vi.fn()
    const navigation = guard(target, null, next)
    request.resolve({ ...context, authorization: { role_assignments: [] } })
    await navigation
    expect(next).toHaveBeenCalledWith({ name: 'AccessDenied', replace: true })
  })

  it('cancels navigation on an authority outage and permits it after retry', async () => {
    const request = rotate(), next = vi.fn()
    const navigation = guard(target, null, next)
    request.reject(Object.assign(new Error('unavailable'), { response: { status: 503 } }))
    await navigation
    expect(next).toHaveBeenCalledWith(false)
    expect(store.token).toBe('new-token')
    next.mockClear()
    await guard(target, null, next)
    expect(next).toHaveBeenCalledWith(false)
    await store.fetchAuthContext()
    next.mockClear()
    await guard(target, null, next)
    expect(next).toHaveBeenCalledWith()
  })

  it('redirects a genuinely invalid session to login instead of a denied page', async () => {
    const request = rotate(), next = vi.fn()
    const navigation = guard(target, null, next)
    request.reject(Object.assign(new Error('unauthenticated'), { response: { status: 401 } }))
    await navigation
    expect(store.token).toBeNull()
    expect(next).toHaveBeenCalledWith({ name: 'Login', query: { redirect: target.fullPath } })
  })

  it('waits for the latest request without letting a stale 401 log out the new session', async () => {
    const oldRequest = rotate(), next = vi.fn()
    const navigation = guard(target, null, next)
    await new Promise(resolve => setTimeout(resolve, 0))
    const newRequest = rotate('latest-token')
    oldRequest.reject(Object.assign(new Error('stale token'), { response: { status: 401 } }))
    await vi.waitFor(() => expect(store.authContextLoadPromise).not.toBeNull())
    expect(next).not.toHaveBeenCalled()
    newRequest.resolve(context)
    await navigation
    expect(store.token).toBe('latest-token')
    expect(next).toHaveBeenCalledWith()
  })
})
