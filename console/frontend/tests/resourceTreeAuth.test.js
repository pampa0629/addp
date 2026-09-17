import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, defineStore, setActivePinia } from 'pinia'

describe('host session integration with shared resource tree requests', () => {
  let auth, tree, store, adapter, transport, refresh
  const requests = []

  beforeEach(async () => {
    vi.resetModules()
    setActivePinia(createPinia())
    vi.stubGlobal('localStorage', { removeItem: vi.fn() })
    requests.length = 0
    const { default: axios } = await import('axios')
    adapter = vi.fn(async config => {
      requests.push({ url: config.url, method: config.method, params: config.params,
        token: config.headers.Authorization, baseURL: config.baseURL })
      if (config.headers.Authorization !== 'Bearer fresh-token') {
        throw Object.assign(new Error('unauthorized'), { config, response: { status: 401 } })
      }
      return { config, status: 200, data: { data: [{ id: 2, name: 'PG', engine_type: 'postgresql' }] } }
    })
    axios.defaults.adapter = adapter
    auth = await import('../../../common-frontend/basic/src/composables/useAuth.js')
    transport = await import('../../../common-frontend/basic/src/auth/authSession.js')
    tree = await import('../../../common-frontend/basic/src/api/resourceTree.js')
    store = defineStore('resource-auth', auth.createAuthStore('resource-auth', {}, { persistUser: false }))()
    store.bindAuthSession()
    transport.setRuntimeAccessToken('expired-token')
    refresh = vi.spyOn(store, 'refreshAccessToken').mockImplementation(async () => {
      transport.setRuntimeAccessToken('fresh-token')
      return 'fresh-token'
    })
  })

  afterEach(() => {
    transport?.clearRuntimeAccessToken()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it.each([
    ['engines', api => api.listResourceTreeEngines('/api/v1/meta')],
    ['root', api => api.getResourceTree('/api/v1/meta', 2)],
    ['node', api => api.getResourceTreeNode('/api/v1/meta', 2, 'resource-locator')],
    ['ancestors', api => api.getResourceTreeAncestors('/api/v1/meta', 2, 'resource-locator')],
    ['search', api => api.searchResourceTree('/api/v1/meta', 2, 'customers')],
    ['refresh', api => api.refreshResourceTreeNode('/api/v1/meta', 2, 'resource-locator')]
  ])('retries %s once with the host replacement token and preserves its contract', async (_name, call) => {
    expect(await call(tree)).toEqual([{ id: 2, name: 'PG', engine_type: 'postgresql' }])
    expect(refresh).toHaveBeenCalledExactlyOnceWith({ force: true })
    expect(requests).toHaveLength(2)
    expect(requests[1]).toEqual({ ...requests[0], token: 'Bearer fresh-token' })
    expect(requests[0].baseURL).toBe('')
  })

  it('coalesces simultaneous resource, module and Fetch 401s on the same host store', async () => {
    let finishRefresh
    refresh.mockImplementation(() => new Promise(resolve => { finishRefresh = () => {
      transport.setRuntimeAccessToken('fresh-token')
      resolve('fresh-token')
    } }))
    const client = auth.createAPIClient(() => store, { moduleName: 'Console' })
    const fetch = vi.fn(async (_url, init) => ({ status: init.headers.get('Authorization') === 'Bearer fresh-token' ? 200 : 401 }))
    const authenticatedFetch = auth.createAuthenticatedFetch(() => store, { moduleName: 'Console', fetch })
    const pending = Promise.allSettled([
      tree.listResourceTreeEngines('/api/v1/meta'),
      tree.getResourceTree('/api/v1/meta', 2),
      tree.getResourceTreeAncestors('/api/v1/meta', 2, 'resource-locator'),
      client.get('/console-test'),
      authenticatedFetch('/stream-test')
    ])
    await vi.waitFor(() => expect(requests).toHaveLength(4))
    expect(refresh).toHaveBeenCalledTimes(1)
    finishRefresh()
    expect((await pending).map(result => result.status)).toEqual(Array(5).fill('fulfilled'))
    expect(requests).toHaveLength(8)
    expect(fetch).toHaveBeenCalledTimes(2)
  })

  it('stops after a second 401 and clears the invalid host session', async () => {
    adapter.mockImplementation(async config => {
      throw Object.assign(new Error('unauthorized'), { config, response: { status: 401 } })
    })
    const clear = vi.spyOn(store, 'clearLocalSession')
    await expect(tree.listResourceTreeEngines('/api/v1/meta')).rejects.toMatchObject({ response: { status: 401 } })
    expect(adapter).toHaveBeenCalledTimes(2)
    expect(refresh).toHaveBeenCalledTimes(1)
    expect(clear).toHaveBeenCalledTimes(1)
  })

  it('keeps the session when refresh has a transient failure and can retry later', async () => {
    const failure = Object.assign(new Error('unavailable'), { response: { status: 503 } })
    refresh.mockRejectedValueOnce(failure)
    const clear = vi.spyOn(store, 'clearLocalSession')
    await expect(tree.listResourceTreeEngines('/api/v1/meta')).rejects.toBe(failure)
    expect(clear).not.toHaveBeenCalled()
    expect(transport.getAccessToken()).toBe('expired-token')
    await tree.listResourceTreeEngines('/api/v1/meta')
    expect(refresh).toHaveBeenCalledTimes(2)
  })

  it('does not refresh or retry a 403', async () => {
    adapter.mockImplementation(async config => {
      throw Object.assign(new Error('forbidden'), { config, response: { status: 403 } })
    })
    await expect(tree.listResourceTreeEngines('/api/v1/meta')).rejects.toMatchObject({ response: { status: 403 } })
    expect(adapter).toHaveBeenCalledTimes(1)
    expect(refresh).not.toHaveBeenCalled()
  })

  it('rejects an uninitialized host before sending a request', async () => {
    vi.resetModules()
    const { default: axios } = await import('axios')
    axios.defaults.adapter = adapter
    const unboundTree = await import('../../../common-frontend/basic/src/api/resourceTree.js')
    await expect(unboundTree.listResourceTreeEngines('/api/v1/meta')).rejects.toThrow('auth_store_not_bound')
    expect(adapter).not.toHaveBeenCalled()
  })

  it('clears the session when the refresh itself is rejected', async () => {
    refresh.mockRejectedValueOnce(Object.assign(new Error('revoked'), { response: { status: 401 } }))
    const clear = vi.spyOn(store, 'clearLocalSession')
    await expect(tree.listResourceTreeEngines('/api/v1/meta')).rejects.toMatchObject({ response: { status: 401 } })
    expect(adapter).toHaveBeenCalledTimes(1)
    expect(clear).toHaveBeenCalledTimes(1)
    expect(transport.getAccessToken()).toBeNull()
  })
})
