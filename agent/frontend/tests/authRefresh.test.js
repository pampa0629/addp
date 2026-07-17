import { describe, expect, it, vi } from 'vitest'

import { createRefreshInterceptor } from '@common-ui'


describe('shared ADDP token refresh', () => {
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
})
