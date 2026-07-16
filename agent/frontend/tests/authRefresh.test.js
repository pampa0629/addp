import { describe, expect, it, vi } from 'vitest'

import { createRefreshInterceptor } from '@common-ui'


describe('shared ADDP token refresh', () => {
  it('retries an Axios request through its owning client after refresh', async () => {
    const authStore = {
      token: 'expired-token',
      setToken: vi.fn((token) => { authStore.token = token }),
      logout: vi.fn()
    }
    const fetchMock = vi.fn(async () => new Response(
      JSON.stringify({ access_token: 'fresh-token' }),
      { status: 200, headers: { 'Content-Type': 'application/json' } }
    ))
    const axiosInstance = vi.fn(async config => ({ data: 'ok', config }))
    const onRefreshFailed = vi.fn()
    const [, onRejected] = createRefreshInterceptor(() => authStore, {
      moduleName: 'Test',
      systemBaseURL: '',
      fetch: fetchMock,
      axiosInstance,
      onRefreshFailed
    })
    const config = { url: '/agent/sessions', headers: {} }

    const response = await onRejected({ response: { status: 401 }, config })

    expect(response.data).toBe('ok')
    expect(config._retry).toBe(true)
    expect(config.headers.Authorization).toBe('Bearer fresh-token')
    expect(axiosInstance).toHaveBeenCalledWith(config)
    expect(authStore.setToken).toHaveBeenCalledWith('fresh-token')
    expect(authStore.logout).not.toHaveBeenCalled()
    expect(onRefreshFailed).not.toHaveBeenCalled()
  })
})
