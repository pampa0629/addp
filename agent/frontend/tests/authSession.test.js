import { beforeEach, describe, expect, it, vi } from 'vitest'

import {
  clearRuntimeAccessToken,
  createBrowserAuthSession,
  createIframeAuthCoordinator,
  getAccessToken,
  getAccessTokenExpiresAt,
  setRuntimeAccessToken
} from '@common-ui/auth/authSession'

describe('BrowserAuthSession', () => {
  beforeEach(() => {
    const values = new Map()
    Object.defineProperty(globalThis, 'localStorage', {
      configurable: true,
      value: {
        clear: () => values.clear(),
        getItem: (key) => values.get(key) || null,
        setItem: (key, value) => values.set(key, String(value)),
        removeItem: (key) => values.delete(key)
      }
    })
    clearRuntimeAccessToken()
    localStorage.clear()
  })

  it('restores a session into memory without persisting the access token', async () => {
    const refresh = vi.fn(async () => ({
      data: { access_token: 'memory-token', expires_in: 900 }
    }))
    const session = createBrowserAuthSession({ refresh })

    await session.initialize()

    expect(refresh).toHaveBeenCalledTimes(1)
    expect(getAccessToken()).toBe('memory-token')
    expect(getAccessTokenExpiresAt()).toBeGreaterThan(Date.now())
    expect(localStorage.getItem('token')).toBeNull()
    session.dispose()
  })

  it('coalesces concurrent refresh calls in one runtime', async () => {
    let resolveRefresh
    const refresh = vi.fn(() => new Promise((resolve) => { resolveRefresh = resolve }))
    const session = createBrowserAuthSession({ refresh })

    const first = session.refreshAccessToken({ force: true })
    const second = session.refreshAccessToken({ force: true })
    await vi.waitFor(() => expect(refresh).toHaveBeenCalledTimes(1))
    resolveRefresh({ data: { access_token: 'rotated-token', expires_in: 900 } })

    await expect(first).resolves.toBe('rotated-token')
    await expect(second).resolves.toBe('rotated-token')
    expect(refresh).toHaveBeenCalledTimes(1)
    session.dispose()
  })

  it('serializes refresh across two browser sessions', async () => {
    let resolveRefresh
    const refresh = vi.fn(() => new Promise((resolve) => { resolveRefresh = resolve }))
    const firstSession = createBrowserAuthSession({ refresh })
    const secondSession = createBrowserAuthSession({ refresh })

    const first = firstSession.refreshAccessToken({ force: true })
    const second = secondSession.refreshAccessToken({ force: true })
    await vi.waitFor(() => expect(refresh).toHaveBeenCalledTimes(1))
    resolveRefresh({ data: { access_token: 'shared-rotated-token', expires_in: 900 } })

    await expect(first).resolves.toBe('shared-rotated-token')
    await expect(second).resolves.toBe('shared-rotated-token')
    expect(refresh).toHaveBeenCalledTimes(1)
    firstSession.dispose()
    secondSession.dispose()
  })

  it('clears the in-memory token after a broadcast logout', async () => {
    const channels = []
    class FakeBroadcastChannel {
      constructor() {
        this.listener = null
        channels.push(this)
      }

      addEventListener(_type, listener) {
        this.listener = listener
      }

      postMessage() {}
      close() {}
    }
    vi.stubGlobal('BroadcastChannel', FakeBroadcastChannel)
    const session = createBrowserAuthSession()
    session.acceptToken('active-token', 900)

    channels[0].listener({ data: { type: 'logout', sender: 'another-tab' } })

    expect(getAccessToken()).toBeNull()
    session.dispose()
    vi.unstubAllGlobals()
  })

  it('shares tokens only with trusted iframe origins and sources', async () => {
    const trustedOrigin = 'https://module.example'
    const iframe = document.createElement('iframe')
    document.body.appendChild(iframe)
    const postMessage = vi.spyOn(iframe.contentWindow, 'postMessage').mockImplementation(() => {})
    const refreshToken = vi.fn()
    const coordinator = createIframeAuthCoordinator({
      allowedOrigins: [trustedOrigin],
      getToken: getAccessToken,
      getExpiresAt: getAccessTokenExpiresAt,
      refreshToken,
      logout: vi.fn()
    })
    setRuntimeAccessToken('iframe-token', Date.now() + 900_000)

    window.dispatchEvent(new MessageEvent('message', {
      data: { protocol: 'addp-auth/v1', type: 'addp-auth-ready', requestId: 'trusted-request' },
      origin: trustedOrigin,
      source: iframe.contentWindow
    }))
    await vi.waitFor(() => expect(postMessage).toHaveBeenCalledTimes(1))
    expect(postMessage).toHaveBeenCalledWith(expect.objectContaining({
      protocol: 'addp-auth/v1',
      type: 'addp-auth-token',
      requestId: 'trusted-request',
      token: 'iframe-token'
    }), trustedOrigin)

    window.dispatchEvent(new MessageEvent('message', {
      data: { protocol: 'addp-auth/v1', type: 'addp-auth-ready', requestId: 'bad-origin' },
      origin: 'https://attacker.example',
      source: iframe.contentWindow
    }))
    window.dispatchEvent(new MessageEvent('message', {
      data: { protocol: 'addp-auth/v1', type: 'addp-auth-ready', requestId: 'bad-source' },
      origin: trustedOrigin,
      source: window
    }))

    await Promise.resolve()
    expect(postMessage).toHaveBeenCalledTimes(1)
    expect(refreshToken).not.toHaveBeenCalled()
    coordinator.dispose()
    iframe.remove()
  })
})
