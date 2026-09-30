import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, defineStore, setActivePinia } from 'pinia'

describe('shared AuthStore role expiry', () => {
  let store, api, browserWindow, browserDocument, transport, context
  const start = new Date('2026-09-30T00:00:00Z').getTime()
  const scope = { type: 'tenant', tenant_id: '3' }

  beforeEach(async () => {
    vi.resetModules()
    vi.useFakeTimers()
    vi.setSystemTime(start)
    setActivePinia(createPinia())
    browserWindow = new EventTarget()
    browserWindow.self = browserWindow
    browserWindow.top = browserWindow
    browserWindow.location = { pathname: '/', assign: vi.fn() }
    browserDocument = new EventTarget()
    browserDocument.visibilityState = 'visible'
    vi.stubGlobal('window', browserWindow)
    vi.stubGlobal('document', browserDocument)
    vi.stubGlobal('localStorage', { removeItem: vi.fn() })
    vi.stubGlobal('BroadcastChannel', undefined)
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    context = {
      context: { type: 'tenant', tenant_id: '3' },
      authorization: { role_assignments: [
        { scope, permissions: ['transfer.task.read'], valid_until: new Date(start + 120_000).toISOString() },
        { scope, permissions: ['monitor.statistics.read'], valid_until: null }
      ] }
    }
    api = { getAuthContext: vi.fn(async () => context) }
    const auth = await import('../../../common-frontend/basic/src/composables/useAuth.js')
    transport = await import('../../../common-frontend/basic/src/auth/authSession.js')
    store = defineStore('expiry-auth', auth.createAuthStore('expiry-auth', api, { persistUser: false }))()
    store.setToken('valid-token')
    await store.fetchAuthContext()
    store.sessionInitialized = true
    store.sessionStatus = 'authenticated'
  })

  afterEach(() => {
    store?.$dispose()
    transport?.clearRuntimeAccessToken()
    vi.useRealTimers()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('withdraws only the expired permissions before the authority response arrives', async () => {
    let resolveContext
    api.getAuthContext.mockImplementationOnce(() => new Promise(resolve => { resolveContext = resolve }))
    await vi.advanceTimersByTimeAsync(119_999)
    expect(store.hasPermission('transfer.task.read')).toBe(true)
    await vi.advanceTimersByTimeAsync(1)
    expect(api.getAuthContext).toHaveBeenCalledTimes(2)
    expect(store.permissions).toEqual(['monitor.statistics.read'])
    expect(store.sessionStatus).toBe('authenticated')
    resolveContext({ ...context, authorization: { role_assignments: [context.authorization.role_assignments[1]] } })
    await store.authContextLoadPromise
    expect(store.permissions).toEqual(['monitor.statistics.read'])
    expect(store.token).toBe('valid-token')
  })

  it('coalesces recovery checks after suspended timers and keeps unrelated focus changes inert', async () => {
    const permissions = store.permissions
    vi.setSystemTime(start + 1000)
    browserWindow.dispatchEvent(new Event('focus'))
    expect(store.permissions).toBe(permissions)
    expect(api.getAuthContext).toHaveBeenCalledTimes(1)
    vi.setSystemTime(start + 180_000)
    browserDocument.visibilityState = 'hidden'
    browserWindow.dispatchEvent(new Event('focus'))
    expect(api.getAuthContext).toHaveBeenCalledTimes(1)
    let resolveContext
    api.getAuthContext.mockImplementationOnce(() => new Promise(resolve => { resolveContext = resolve }))
    browserDocument.visibilityState = 'visible'
    browserDocument.dispatchEvent(new Event('visibilitychange'))
    browserWindow.dispatchEvent(new Event('focus'))
    expect(api.getAuthContext).toHaveBeenCalledTimes(2)
    expect(store.permissions).toEqual(['monitor.statistics.read'])
    resolveContext(context)
    await store.authContextLoadPromise
    expect(store.hasPermission('transfer.task.read')).toBe(false)
  })

  it('keeps expiry effective through a transient failure and recovers on focus', async () => {
    api.getAuthContext.mockRejectedValueOnce(Object.assign(new Error('unavailable'), { response: { status: 503 } }))
    await vi.advanceTimersByTimeAsync(120_000)
    expect(store.permissions).toEqual(['monitor.statistics.read'])
    expect(store.token).toBe('valid-token')
    expect(store.sessionStatus).toBe('authenticated')
    context = { ...context, authorization: { role_assignments: [context.authorization.role_assignments[1]] } }
    browserWindow.dispatchEvent(new Event('focus'))
    await store.authContextLoadPromise
    expect(api.getAuthContext).toHaveBeenCalledTimes(3)
    expect(store.authContext.authorization.role_assignments).toHaveLength(1)
  })

  it('cancels the old deadline when a replacement context is loaded', async () => {
    context = { context: { type: 'tenant', tenant_id: '4' }, authorization: { role_assignments: [
      { scope: { type: 'tenant', tenant_id: '4' }, permissions: ['transfer.task.read'], valid_until: null }
    ] } }
    store.setToken('replacement-token')
    await store.authContextLoadPromise
    await vi.advanceTimersByTimeAsync(120_000)
    expect(api.getAuthContext).toHaveBeenCalledTimes(2)
    expect(store.permissions).toEqual(['transfer.task.read'])
  })

  it('cleans up timers and resume listeners on logout and disposal', async () => {
    store.clearLocalSession()
    await vi.advanceTimersByTimeAsync(120_000)
    browserWindow.dispatchEvent(new Event('focus'))
    expect(api.getAuthContext).toHaveBeenCalledTimes(1)
    store.setToken('valid-token')
    await store.fetchAuthContext()
    store.$dispose()
    await vi.advanceTimersByTimeAsync(120_000)
    browserDocument.dispatchEvent(new Event('visibilitychange'))
    expect(api.getAuthContext).toHaveBeenCalledTimes(2)
  })

  it('does not overflow browser timers for a distant role expiry', async () => {
    context = { ...context, authorization: { role_assignments: [{
      scope, permissions: ['transfer.task.read'], valid_until: new Date(start + 4_000_000_000).toISOString()
    }] } }
    await store.fetchAuthContext()
    await vi.advanceTimersByTimeAsync(2_147_483_647)
    expect(api.getAuthContext).toHaveBeenCalledTimes(2)
    expect(store.permissions).toEqual(['transfer.task.read'])
    await vi.advanceTimersByTimeAsync(4_000_000_000 - 2_147_483_647)
    expect(api.getAuthContext).toHaveBeenCalledTimes(3)
    expect(store.permissions).toEqual([])
  })

  it('ignores an authority response arriving after store disposal', async () => {
    let resolveContext
    api.getAuthContext.mockImplementationOnce(() => new Promise(resolve => { resolveContext = resolve }))
    const pending = store.fetchAuthContext({ force: true })
    store.$dispose()
    resolveContext({ ...context, authorization: { role_assignments: [] } })
    expect(await pending).toBeNull()
    expect(store.authContext.authorization.role_assignments).toHaveLength(2)
    expect(vi.getTimerCount()).toBe(0)
  })
})
