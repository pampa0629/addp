const CHANNEL_NAME = 'addp-auth-session'
const REFRESH_LOCK_NAME = 'addp-auth-refresh'
const FALLBACK_LOCK_KEY = 'addp-auth-refresh-lock'
const PROTOCOL = 'addp-auth/v1'
const REFRESH_EARLY_MS = 60_000
const FALLBACK_LOCK_TTL_MS = 15_000

let runtimeToken = null
let runtimeExpiresAt = 0
const runtimeListeners = new Set()

function now() {
  return Date.now()
}

function randomID() {
  if (globalThis.crypto?.randomUUID) return globalThis.crypto.randomUUID()
  return `${now()}-${Math.random().toString(36).slice(2)}`
}

function isBrowser() {
  return typeof window !== 'undefined' && typeof document !== 'undefined'
}

function isEmbedded() {
  return isBrowser() && window.self !== window.top
}

function parentOrigin() {
  if (!isEmbedded() || !document.referrer) return null
  try {
    return new URL(document.referrer).origin
  } catch {
    return null
  }
}

function notifyRuntimeListeners(source) {
  const snapshot = {
    token: runtimeToken,
    expiresAt: runtimeExpiresAt,
    source
  }
  runtimeListeners.forEach((listener) => listener(snapshot))
}

function applyRuntimeToken(token, expiresAt, source = 'local') {
  runtimeToken = token || null
  runtimeExpiresAt = token ? Number(expiresAt || 0) : 0
  notifyRuntimeListeners(source)
}

export function getAccessToken() {
  return runtimeToken
}

export function getAccessTokenExpiresAt() {
  return runtimeExpiresAt
}

export function subscribeAccessToken(listener) {
  runtimeListeners.add(listener)
  return () => runtimeListeners.delete(listener)
}

export function setRuntimeAccessToken(token, expiresAt = 0) {
  applyRuntimeToken(token, expiresAt, 'runtime-provider')
}

export function clearRuntimeAccessToken() {
  applyRuntimeToken(null, 0, 'runtime-provider')
}

function isUsableToken(token = runtimeToken, expiresAt = runtimeExpiresAt, marginMs = 5_000) {
  return Boolean(token) && (!expiresAt || expiresAt > now() + marginMs)
}

function isAuthFailure(error) {
  return error?.response?.status === 401 || error?.status === 401
}

function createChannel(onMessage) {
  if (!isBrowser() || typeof BroadcastChannel === 'undefined' || isEmbedded()) return null
  const channel = new BroadcastChannel(CHANNEL_NAME)
  channel.addEventListener('message', (event) => onMessage(event.data))
  return channel
}

function readFallbackLock() {
  try {
    const value = localStorage.getItem(FALLBACK_LOCK_KEY)
    return value ? JSON.parse(value) : null
  } catch {
    return null
  }
}

function writeFallbackLock(value) {
  try {
    localStorage.setItem(FALLBACK_LOCK_KEY, JSON.stringify(value))
    return true
  } catch {
    return false
  }
}

function removeFallbackLock(owner) {
  try {
    const current = readFallbackLock()
    if (current?.owner === owner) localStorage.removeItem(FALLBACK_LOCK_KEY)
  } catch {
    // A failed cleanup only leaves an expiring coordination lease.
  }
}

function wait(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

async function withFallbackRefreshLock(owner, callback) {
  const deadline = now() + FALLBACK_LOCK_TTL_MS
  while (now() < deadline) {
    const current = readFallbackLock()
    if (!current || current.expiresAt <= now()) {
      const lease = { owner, expiresAt: now() + FALLBACK_LOCK_TTL_MS }
      if (writeFallbackLock(lease)) {
        await wait(25)
        if (readFallbackLock()?.owner === owner) {
          try {
            return await callback()
          } finally {
            removeFallbackLock(owner)
          }
        }
      }
    }
    await wait(50)
  }
  throw new Error('auth_refresh_lock_timeout')
}

async function withRefreshLock(owner, callback) {
  if (isBrowser() && navigator.locks?.request) {
    return navigator.locks.request(REFRESH_LOCK_NAME, { mode: 'exclusive' }, callback)
  }
  return withFallbackRefreshLock(owner, callback)
}

export function createBrowserAuthSession({ refresh, revoke } = {}) {
  const instanceID = randomID()
  let refreshPromise = null
  let initializePromise = null
  let proactiveTimer = null
  let disposed = false
  let tokenRequestResolvers = new Map()
  let parentRequestResolvers = new Map()

  const broadcast = (message) => channel?.postMessage({ ...message, sender: instanceID })

  const publishToken = (token, expiresAt, source = 'local', broadcastEvent = true) => {
    applyRuntimeToken(token, expiresAt, source)
    scheduleProactiveRefresh()
    if (broadcastEvent && !isEmbedded()) {
      broadcast({ type: 'token', token, expiresAt })
    }
  }

  const clearToken = ({ broadcastEvent = true, source = 'local' } = {}) => {
    applyRuntimeToken(null, 0, source)
    if (proactiveTimer) clearTimeout(proactiveTimer)
    proactiveTimer = null
    if (broadcastEvent && !isEmbedded()) broadcast({ type: 'logout' })
  }

  const onChannelMessage = (message) => {
    if (!message || message.sender === instanceID) return
    if (message.type === 'token' && message.token) {
      publishToken(message.token, message.expiresAt, 'broadcast', false)
      tokenRequestResolvers.forEach((resolve) => resolve(message.token))
      tokenRequestResolvers.clear()
      return
    }
    if (message.type === 'token-request' && isUsableToken()) {
      broadcast({ type: 'token', token: runtimeToken, expiresAt: runtimeExpiresAt })
      return
    }
    if (message.type === 'logout' || message.type === 'session-invalid') {
      clearToken({ broadcastEvent: false, source: 'broadcast' })
    }
  }

  const channel = createChannel(onChannelMessage)

  const onParentMessage = (event) => {
    const expectedOrigin = parentOrigin()
    if (!isEmbedded() || !expectedOrigin || event.source !== window.parent || event.origin !== expectedOrigin) return
    const message = event.data
    if (!message || message.protocol !== PROTOCOL) return
    if (message.type === 'addp-auth-token' && message.token) {
      publishToken(message.token, message.expiresAt, 'parent', false)
      const pending = parentRequestResolvers.get(message.requestId)
      if (pending) {
        parentRequestResolvers.delete(message.requestId)
        clearTimeout(pending.timer)
        pending.resolve(message.token)
      }
      return
    }
    if (message.type === 'addp-auth-logout') {
      clearToken({ broadcastEvent: false, source: 'parent' })
      parentRequestResolvers.forEach(({ reject, timer }) => {
        clearTimeout(timer)
        reject(new Error('parent_session_unavailable'))
      })
      parentRequestResolvers.clear()
    }
  }

  if (isBrowser()) window.addEventListener('message', onParentMessage)

  function acceptToken(token, expiresInSeconds = 0, { absoluteExpiresAt = 0 } = {}) {
    const expiresAt = absoluteExpiresAt || (expiresInSeconds ? now() + Number(expiresInSeconds) * 1000 : 0)
    publishToken(token, expiresAt)
    return token
  }

  function requestPeerToken(timeoutMs = 150) {
    if (!channel) return Promise.resolve(null)
    const requestID = randomID()
    return new Promise((resolve) => {
      const timer = setTimeout(() => {
        tokenRequestResolvers.delete(requestID)
        resolve(null)
      }, timeoutMs)
      tokenRequestResolvers.set(requestID, (token) => {
        clearTimeout(timer)
        resolve(token)
      })
      broadcast({ type: 'token-request', requestId: requestID })
    })
  }

  function requestParentToken({ forceRefresh = false, timeoutMs = 8_000 } = {}) {
    const expectedOrigin = parentOrigin()
    if (!isEmbedded() || !expectedOrigin) return Promise.reject(new Error('trusted_parent_origin_required'))
    const requestID = randomID()
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        parentRequestResolvers.delete(requestID)
        reject(new Error('parent_auth_timeout'))
      }, timeoutMs)
      parentRequestResolvers.set(requestID, { resolve, reject, timer })
      window.parent.postMessage({
        protocol: PROTOCOL,
        type: forceRefresh ? 'addp-auth-refresh-request' : 'addp-auth-ready',
        requestId: requestID
      }, expectedOrigin)
    })
  }

  async function refreshAccessToken({ force = false } = {}) {
    if (isEmbedded()) return requestParentToken({ forceRefresh: force })
    if (refreshPromise) return refreshPromise
    const observedExpiresAt = runtimeExpiresAt
    refreshPromise = withRefreshLock(instanceID, async () => {
      if (!force && isUsableToken()) return runtimeToken
      if (runtimeExpiresAt > observedExpiresAt && isUsableToken()) return runtimeToken

      const peerToken = await requestPeerToken(100)
      if (peerToken && isUsableToken()) return peerToken
      if (typeof refresh !== 'function') throw new Error('auth_refresh_not_configured')

      try {
        const response = await refresh()
        const payload = response?.data || response
        if (!payload?.access_token) throw new Error('refresh_response_missing_access_token')
        return acceptToken(payload.access_token, payload.expires_in)
      } catch (error) {
        if (isAuthFailure(error)) {
          clearToken({ broadcastEvent: false, source: 'refresh-rejected' })
          broadcast({ type: 'session-invalid' })
        }
        throw error
      }
    }).finally(() => {
      refreshPromise = null
    })
    return refreshPromise
  }

  function scheduleProactiveRefresh() {
    if (proactiveTimer) clearTimeout(proactiveTimer)
    proactiveTimer = null
    if (disposed || isEmbedded() || !runtimeToken || !runtimeExpiresAt) return
    const delay = Math.max(1_000, runtimeExpiresAt - now() - REFRESH_EARLY_MS)
    proactiveTimer = setTimeout(async () => {
      try {
        await refreshAccessToken({ force: true })
      } catch (error) {
        if (!isAuthFailure(error) && runtimeTokenExpiresSoon()) {
          proactiveTimer = setTimeout(() => refreshAccessToken({ force: true }).catch(() => {}), 30_000)
        }
      }
    }, delay)
  }

  function runtimeTokenExpiresSoon() {
    return Boolean(runtimeExpiresAt) && runtimeExpiresAt <= now() + 90_000
  }

  async function initialize() {
    if (isUsableToken()) return runtimeToken
    if (initializePromise) return initializePromise
    initializePromise = (async () => {
      if (isEmbedded()) return requestParentToken()
      const peerToken = await requestPeerToken()
      if (peerToken && isUsableToken()) return peerToken
      return refreshAccessToken({ force: true })
    })().finally(() => {
      initializePromise = null
    })
    return initializePromise
  }

  async function logout() {
    if (isEmbedded()) {
      const expectedOrigin = parentOrigin()
      if (expectedOrigin) {
        window.parent.postMessage({ protocol: PROTOCOL, type: 'addp-auth-logout-request' }, expectedOrigin)
      }
      clearToken({ broadcastEvent: false, source: 'iframe-logout' })
      return
    }
    try {
      if (typeof revoke === 'function') await revoke()
    } finally {
      clearToken({ broadcastEvent: true, source: 'logout' })
    }
  }

  function dispose() {
    disposed = true
    if (proactiveTimer) clearTimeout(proactiveTimer)
    channel?.close()
    if (isBrowser()) window.removeEventListener('message', onParentMessage)
    tokenRequestResolvers.clear()
    parentRequestResolvers.forEach(({ timer }) => clearTimeout(timer))
    parentRequestResolvers.clear()
  }

  return {
    acceptToken,
    clearToken,
    initialize,
    refreshAccessToken,
    logout,
    dispose,
    isEmbedded
  }
}

export function createIframeAuthCoordinator({ allowedOrigins, getToken, getExpiresAt, refreshToken, logout }) {
  if (!isBrowser()) return { dispose() {} }
  const trustedOrigins = new Set(allowedOrigins || [])
  const clients = new Map()

  function isTrustedSource(source) {
    return Array.from(document.querySelectorAll('iframe')).some((iframe) => iframe.contentWindow === source)
  }

  function sendToken(target, origin, requestId) {
    const token = getToken()
    if (!token) return false
    target.postMessage({
      protocol: PROTOCOL,
      type: 'addp-auth-token',
      requestId,
      token,
      expiresAt: getExpiresAt()
    }, origin)
    return true
  }

  async function handleMessage(event) {
    const message = event.data
    if (!message || message.protocol !== PROTOCOL || !trustedOrigins.has(event.origin) || !isTrustedSource(event.source)) return
    if (message.type === 'addp-auth-ready' || message.type === 'addp-auth-refresh-request') {
      clients.set(event.source, event.origin)
      if (message.type === 'addp-auth-refresh-request' || !getToken()) {
        try {
          await refreshToken({ force: true })
        } catch {
          event.source.postMessage({ protocol: PROTOCOL, type: 'addp-auth-logout' }, event.origin)
          return
        }
      }
      sendToken(event.source, event.origin, message.requestId)
      return
    }
    if (message.type === 'addp-auth-logout-request') await logout()
  }

  window.addEventListener('message', handleMessage)
  const unsubscribe = subscribeAccessToken(({ token }) => {
    clients.forEach((origin, target) => {
      if (token) sendToken(target, origin)
      else target.postMessage({ protocol: PROTOCOL, type: 'addp-auth-logout' }, origin)
    })
  })

  return {
    dispose() {
      window.removeEventListener('message', handleMessage)
      unsubscribe()
      clients.clear()
    }
  }
}
