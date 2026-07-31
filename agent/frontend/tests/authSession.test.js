import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

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

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
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

  it('does not reuse a peer token during a forced refresh', async () => {
    const channels = []
    class FakeBroadcastChannel {
      constructor() {
        this.listener = null
        channels.push(this)
      }

      addEventListener(_type, listener) {
        this.listener = listener
      }

      postMessage(message) {
        if (message.type !== 'token-request') return
        queueMicrotask(() => this.listener({
          data: {
            type: 'token',
            sender: 'another-tab',
            token: 'revoked-peer-token',
            expiresAt: Date.now() + 900_000
          }
        }))
      }

      close() {}
    }
    vi.stubGlobal('BroadcastChannel', FakeBroadcastChannel)
    const refresh = vi.fn(async () => ({
      data: { access_token: 'server-refreshed-token', expires_in: 900 }
    }))
    const session = createBrowserAuthSession({ refresh })

    await expect(session.refreshAccessToken({ force: true })).resolves.toBe('server-refreshed-token')
    expect(channels).toHaveLength(1)
    expect(refresh).toHaveBeenCalledTimes(1)
    session.dispose()
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

  it('coalesces repeated iframe refresh messages with the same request id', async () => {
    const trustedOrigin = 'https://module.example'
    const iframe = document.createElement('iframe')
    document.body.appendChild(iframe)
    const postMessage = vi.spyOn(iframe.contentWindow, 'postMessage').mockImplementation(() => {})
    let resolveRefresh
    const refreshResult = new Promise(resolve => { resolveRefresh = resolve })
    const refreshToken = vi.fn(() => refreshResult)
    const coordinator = createIframeAuthCoordinator({
      allowedOrigins: [trustedOrigin],
      getToken: getAccessToken,
      getExpiresAt: getAccessTokenExpiresAt,
      refreshToken,
      logout: vi.fn()
    })
    const event = () => new MessageEvent('message', {
      data: {
        protocol: 'addp-auth/v1',
        type: 'addp-auth-refresh-request',
        requestId: 'repeated-refresh'
      },
      origin: trustedOrigin,
      source: iframe.contentWindow
    })

    window.dispatchEvent(event())
    window.dispatchEvent(event())

    await vi.waitFor(() => expect(refreshToken).toHaveBeenCalledTimes(1))
    setRuntimeAccessToken('coalesced-token', Date.now() + 900_000)
    resolveRefresh('coalesced-token')
    await vi.waitFor(() => expect(postMessage).toHaveBeenCalledWith(expect.objectContaining({
      type: 'addp-auth-token',
      requestId: 'repeated-refresh',
      token: 'coalesced-token'
    }), trustedOrigin))

    coordinator.dispose()
    iframe.remove()
  })

  it('propagates a rejected parent refresh as an authentication failure', async () => {
    const parent = { postMessage: vi.fn() }
    const listeners = new Map()
    const childWindow = {
      self: null,
      top: {},
      parent,
      addEventListener: vi.fn((type, listener) => listeners.set(type, listener)),
      removeEventListener: vi.fn()
    }
    childWindow.self = childWindow
    vi.stubGlobal('window', childWindow)
    vi.stubGlobal('document', { referrer: 'https://console.example/system/iam' })
    const session = createBrowserAuthSession()

    const refresh = session.refreshAccessToken({ force: true })
    const request = parent.postMessage.mock.calls[0][0]
    listeners.get('message')({
      data: {
        protocol: 'addp-auth/v1',
        type: 'addp-auth-logout',
        requestId: request.requestId,
        error_code: 'authentication_required'
      },
      origin: 'https://console.example',
      source: parent
    })

    await expect(refresh).rejects.toMatchObject({
      status: 401,
      code: 'authentication_required'
    })
    session.dispose()
  })

  it('retries an iframe handshake when the parent misses the first ready message', async () => {
    vi.useFakeTimers()
    const parent = { postMessage: vi.fn() }
    const listeners = new Map()
    const childWindow = {
      self: null,
      top: {},
      parent,
      addEventListener: vi.fn((type, listener) => listeners.set(type, listener)),
      removeEventListener: vi.fn()
    }
    childWindow.self = childWindow
    vi.stubGlobal('window', childWindow)
    vi.stubGlobal('document', { referrer: 'https://console.example/orchestrator/orchestrations' })
    const session = createBrowserAuthSession()

    const initialization = session.initialize()
    expect(parent.postMessage).toHaveBeenCalledTimes(1)
    const firstRequest = parent.postMessage.mock.calls[0][0]

    await vi.advanceTimersByTimeAsync(300)
    expect(parent.postMessage).toHaveBeenCalledTimes(2)
    expect(parent.postMessage.mock.calls[1][0]).toMatchObject({
      type: 'addp-auth-ready',
      requestId: firstRequest.requestId
    })

    listeners.get('message')({
      data: {
        protocol: 'addp-auth/v1',
        type: 'addp-auth-token',
        requestId: firstRequest.requestId,
        token: 'parent-token',
        expiresAt: Date.now() + 900_000
      },
      origin: 'https://console.example',
      source: parent
    })

    await expect(initialization).resolves.toBe('parent-token')
    session.dispose()
  })

  it('reports authentication rejection to the requesting iframe', async () => {
    const trustedOrigin = 'https://module.example'
    const iframe = document.createElement('iframe')
    document.body.appendChild(iframe)
    const postMessage = vi.spyOn(iframe.contentWindow, 'postMessage').mockImplementation(() => {})
    const authFailure = Object.assign(new Error('session revoked'), { response: { status: 401 } })
    const coordinator = createIframeAuthCoordinator({
      allowedOrigins: [trustedOrigin],
      getToken: getAccessToken,
      getExpiresAt: getAccessTokenExpiresAt,
      refreshToken: vi.fn(async () => { throw authFailure }),
      logout: vi.fn()
    })

    window.dispatchEvent(new MessageEvent('message', {
      data: { protocol: 'addp-auth/v1', type: 'addp-auth-refresh-request', requestId: 'refresh-request' },
      origin: trustedOrigin,
      source: iframe.contentWindow
    }))

    await vi.waitFor(() => expect(postMessage).toHaveBeenCalledWith({
      protocol: 'addp-auth/v1',
      type: 'addp-auth-logout',
      requestId: 'refresh-request',
      error_code: 'authentication_required'
    }, trustedOrigin))
    coordinator.dispose()
    iframe.remove()
  })

  it('keeps the iframe session on a temporary parent refresh failure', async () => {
    const trustedOrigin = 'https://module.example'
    const iframe = document.createElement('iframe')
    document.body.appendChild(iframe)
    const postMessage = vi.spyOn(iframe.contentWindow, 'postMessage').mockImplementation(() => {})
    const temporaryFailure = Object.assign(new Error('service unavailable'), { response: { status: 503 } })
    const coordinator = createIframeAuthCoordinator({
      allowedOrigins: [trustedOrigin],
      getToken: getAccessToken,
      getExpiresAt: getAccessTokenExpiresAt,
      refreshToken: vi.fn(async () => { throw temporaryFailure }),
      logout: vi.fn()
    })

    window.dispatchEvent(new MessageEvent('message', {
      data: { protocol: 'addp-auth/v1', type: 'addp-auth-refresh-request', requestId: 'refresh-request' },
      origin: trustedOrigin,
      source: iframe.contentWindow
    }))

    await vi.waitFor(() => expect(postMessage).toHaveBeenCalledWith({
      protocol: 'addp-auth/v1',
      type: 'addp-auth-error',
      requestId: 'refresh-request',
      error_code: 'session_refresh_failed',
      status: 503
    }, trustedOrigin))
    coordinator.dispose()
    iframe.remove()
  })
})
