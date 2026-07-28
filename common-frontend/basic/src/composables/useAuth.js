import axios from 'axios'

import {
  createBrowserAuthSession,
  getAccessToken,
  getAccessTokenExpiresAt,
  subscribeAccessToken
} from '../auth/authSession'

function resolveAuthStore(authStoreOrGetter) {
  return typeof authStoreOrGetter === 'function'
    ? authStoreOrGetter()
    : authStoreOrGetter
}

function isAuthenticationFailure(error) {
  return error?.response?.status === 401 || error?.status === 401
}

export function buildLoginRedirectURL(location = globalThis.location) {
  const pathname = location?.pathname || '/'
  if (pathname === '/login') return null
  const redirect = `${pathname}${location?.search || ''}${location?.hash || ''}`
  return `/login?redirect=${encodeURIComponent(redirect)}`
}

function redirectToLogin() {
  if (typeof window === 'undefined' || window.self !== window.top) return
  const target = buildLoginRedirectURL(window.location)
  if (target) window.location.assign(target)
}

export function collectAuthContextPermissions(authContext) {
  const keys = new Set()
  for (const assignment of authContext?.authorization?.role_assignments || []) {
    for (const permission of assignment.permissions || []) keys.add(permission)
  }
  return [...keys].sort()
}

export function createAuthGuard(authStoreOrGetter, config = {}) {
  const {
    loginRouteName = 'Login',
    normalizeRedirect = (path) => path
  } = config

  return async (to, _from, next) => {
    const authStore = resolveAuthStore(authStoreOrGetter)
    try {
      await authStore.initializeSession({ force: authStore.sessionStatus === 'error' })
    } catch (error) {
      console.error('[Auth] Session initialization failed:', error)
      if (authStore.sessionStatus === 'error') return next()
    }

    const requiresAuth = to.matched.some((record) => record.meta?.requiresAuth !== false)
    const isPublic = to.name === loginRouteName

    if (authStore.isAuthenticated && isPublic) return next('/')
    if (!authStore.isAuthenticated && requiresAuth && !isPublic) {
      return next({
        name: loginRouteName,
        query: { redirect: normalizeRedirect(to.fullPath) }
      })
    }
    next()
  }
}

export function createAuthInterceptor(authStoreOrGetter, _moduleName = 'Module') {
  return async (config) => {
    const authStore = resolveAuthStore(authStoreOrGetter)
    const token = getAccessToken() || authStore.token
    if (token) config.headers.Authorization = `Bearer ${token}`
    else delete config.headers.Authorization
    return config
  }
}

function createAuthStoreConfig(storeName, authAPI, options = {}) {
  const { persistUser = true } = options
  if (typeof localStorage !== 'undefined') localStorage.removeItem('token')
  const authSession = createBrowserAuthSession({
    refresh: () => authAPI.refresh(),
    revoke: () => authAPI.logout()
  })
  let boundStore = null
  let unsubscribe = null

  function bindStore(store) {
    if (boundStore === store) return
    unsubscribe?.()
    boundStore = store
    unsubscribe = subscribeAccessToken(({ token, expiresAt }) => {
      const wasAuthenticated = Boolean(store.token)
      store.token = token
      store.tokenExpiresAt = expiresAt
      if (!token) {
        store.user = null
        store.authContext = null
        store.sessionStatus = 'anonymous'
        if (persistUser) localStorage.removeItem('user')
        if (wasAuthenticated && store.sessionInitialized && !authSession.isEmbedded() && typeof window !== 'undefined') {
          redirectToLogin()
        }
      }
    })
    store.token = getAccessToken()
    store.tokenExpiresAt = getAccessTokenExpiresAt()
  }

  async function acceptLoginResult(store, payload) {
    if (!payload || typeof payload.next_action !== 'string') {
      throw new Error('auth_login_response_invalid')
    }
    switch (payload.next_action) {
      case 'session_issued': {
        const session = payload.session
        if (!session?.access_token) throw new Error('auth_login_session_missing_access_token')
        authSession.acceptToken(session.access_token, session.expires_in)
        await store.fetchSessionState()
        store.sessionInitialized = true
        store.sessionStatus = 'authenticated'
        return payload
      }
      case 'verify_mfa':
        if (!payload.mfa?.challenge_token || payload.mfa.method !== 'totp' || !payload.mfa.expires_at) {
          throw new Error('auth_login_mfa_challenge_invalid')
        }
        return payload
      case 'select_context':
        if (!payload.selection?.selection_ticket || !payload.selection.expires_at ||
            !Array.isArray(payload.selection.contexts) || payload.selection.contexts.length === 0) {
          throw new Error('auth_login_context_selection_invalid')
        }
        return payload
      default:
        throw new Error('auth_login_next_action_unsupported')
    }
  }

  return {
    state: () => ({
      token: null,
      tokenExpiresAt: 0,
      user: persistUser ? (() => {
        const stored = localStorage.getItem('user')
        if (!stored) return null
        try {
          return JSON.parse(stored)
        } catch {
          return null
        }
      })() : null,
      authContext: null,
      isLoadingUser: false,
      userLoadPromise: null,
      authContextLoadPromise: null,
      sessionInitialized: false,
      sessionInitPromise: null,
      sessionStatus: 'idle',
      sessionError: null
    }),

    getters: {
      isAuthenticated: (state) => Boolean(state.token),
      contextType: (state) => state.authContext?.context?.type || null,
      permissions: (state) => collectAuthContextPermissions(state.authContext),
      hasPermission() {
        return (permission) => this.permissions.includes(permission)
      },
      hasAnyPermission() {
        return (permissions) => permissions.some((permission) => this.hasPermission(permission))
      }
    },

    actions: {
      bindAuthSession() {
        bindStore(this)
      },

      async initializeSession({ force = false } = {}) {
        bindStore(this)
        if (this.sessionInitialized && !force) return this.token
        if (this.sessionInitPromise) return this.sessionInitPromise

        this.sessionStatus = 'initializing'
        this.sessionError = null
        this.sessionInitPromise = (async () => {
          try {
            const token = await authSession.initialize()
            if (token) {
              await this.fetchSessionState()
              this.sessionStatus = 'authenticated'
            } else {
              this.sessionStatus = 'anonymous'
            }
            return token
          } catch (error) {
            if (isAuthenticationFailure(error)) {
              this.clearLocalSession()
              this.sessionStatus = 'anonymous'
              return null
            }
            this.sessionStatus = 'error'
            this.sessionError = error
            throw error
          } finally {
            this.sessionInitialized = true
            this.sessionInitPromise = null
          }
        })()
        return this.sessionInitPromise
      },

      async login(username, password) {
        bindStore(this)
        const response = await authAPI.login(username, password)
        const payload = response.data || response
        return acceptLoginResult(this, payload)
      },

      async verifyMFA(challengeToken, code) {
        bindStore(this)
        const response = await authAPI.verifyMFA(challengeToken, code)
        return acceptLoginResult(this, response.data || response)
      },

      async selectContext(selectionTicket, context) {
        bindStore(this)
        const response = await authAPI.selectContext(selectionTicket, context)
        const session = response.data || response
        return acceptLoginResult(this, { next_action: 'session_issued', session })
      },

      setToken(token, expiresIn = 0) {
        bindStore(this)
        if (token) authSession.acceptToken(token, expiresIn)
        else authSession.clearToken({ broadcastEvent: false })
      },

      async refreshAccessToken(options = {}) {
        bindStore(this)
        const token = await authSession.refreshAccessToken(options)
        if (token) this.sessionStatus = 'authenticated'
        return token
      },

      async refreshAuthorization() {
        await this.refreshAccessToken({ force: true })
        return this.fetchAuthContext()
      },

      async fetchUser() {
        if (this.userLoadPromise) return this.userLoadPromise
        const token = getAccessToken() || this.token
        if (!token) {
          this.user = null
          if (persistUser) localStorage.removeItem('user')
          return null
        }

        this.isLoadingUser = true
        this.userLoadPromise = authAPI.getUser(token)
          .then((response) => {
            this.user = response.data || response
            if (persistUser) localStorage.setItem('user', JSON.stringify(this.user))
            return response
          })
          .finally(() => {
            this.isLoadingUser = false
            this.userLoadPromise = null
          })
        return this.userLoadPromise
      },

      async fetchAuthContext() {
        if (this.authContextLoadPromise) return this.authContextLoadPromise
        const token = getAccessToken() || this.token
        if (!token) {
          this.authContext = null
          return null
        }

        this.authContextLoadPromise = authAPI.getAuthContext(token)
          .then((response) => {
            this.authContext = response.data || response
            return this.authContext
          })
          .finally(() => {
            this.authContextLoadPromise = null
          })
        return this.authContextLoadPromise
      },

      async fetchSessionState() {
        const [user] = await Promise.all([this.fetchUser(), this.fetchAuthContext()])
        return user
      },

      async waitForUserLoad() {
        if (this.userLoadPromise) return this.userLoadPromise
        if (this.user) return { data: this.user }
        throw new Error(`${storeName}: user is not loading`)
      },

      clearLocalSession() {
        bindStore(this)
        authSession.clearToken({ broadcastEvent: false })
        this.user = null
        this.authContext = null
        this.isLoadingUser = false
        this.userLoadPromise = null
        this.authContextLoadPromise = null
        this.sessionStatus = 'anonymous'
        if (persistUser) localStorage.removeItem('user')
      },

      async logout() {
        bindStore(this)
        try {
          await authSession.logout()
        } catch (error) {
          console.warn(`[${storeName}] Remote logout failed:`, error)
        } finally {
          this.clearLocalSession()
          this.sessionInitialized = true
        }
      }
    }
  }
}

function createTokenRefresher(authStoreOrGetter) {
  let localRefreshPromise = null
  return async () => {
    if (localRefreshPromise) return localRefreshPromise
    const authStore = resolveAuthStore(authStoreOrGetter)
    localRefreshPromise = authStore.refreshAccessToken({ force: true })
      .finally(() => {
        localRefreshPromise = null
      })
    return localRefreshPromise
  }
}

export function createAuthenticatedFetch(authStoreOrGetter, config = {}) {
  const {
    moduleName,
    fetch: fetchImpl = globalThis.fetch.bind(globalThis),
    onRefreshFailed
  } = config
  if (!moduleName) throw new Error('moduleName is required for createAuthenticatedFetch')

  const refreshToken = createTokenRefresher(authStoreOrGetter)
  return async (input, init = {}) => {
    const isRequest = typeof Request !== 'undefined' && input instanceof Request
    const baseHeaders = new Headers(isRequest ? input.headers : undefined)
    new Headers(init.headers).forEach((value, key) => baseHeaders.set(key, value))
    const withToken = (token) => {
      const headers = new Headers(baseHeaders)
      if (token) headers.set('Authorization', `Bearer ${token}`)
      else headers.delete('Authorization')
      return { ...init, headers }
    }

    const retryInput = isRequest ? input.clone() : input
    const response = await fetchImpl(input, withToken(getAccessToken()))
    if (response.status !== 401) return response

    try {
      const token = await refreshToken()
      return fetchImpl(retryInput, withToken(token))
    } catch (error) {
      if (isAuthenticationFailure(error)) {
        resolveAuthStore(authStoreOrGetter).clearLocalSession()
        onRefreshFailed?.(error)
      }
      throw error
    }
  }
}

export function createRefreshInterceptor(authStoreOrGetter, config = {}) {
  const { axiosInstance, onRefreshFailed } = config
  const refreshToken = createTokenRefresher(authStoreOrGetter)
  const onFulfilled = (response) => response
  const onRejected = async (error) => {
    if (error.response?.status !== 401) return Promise.reject(error)
    const originalRequest = error.config
    if (originalRequest?._retry) {
      resolveAuthStore(authStoreOrGetter).clearLocalSession()
      onRefreshFailed?.(error)
      return Promise.reject(error)
    }
    originalRequest._retry = true
    try {
      const token = await refreshToken()
      originalRequest.headers.Authorization = `Bearer ${token}`
      return axiosInstance(originalRequest)
    } catch (refreshError) {
      if (isAuthenticationFailure(refreshError)) {
        resolveAuthStore(authStoreOrGetter).clearLocalSession()
        onRefreshFailed?.(refreshError)
      }
      return Promise.reject(refreshError)
    }
  }
  return [onFulfilled, onRejected]
}

export function createAuthAPI(client) {
  return {
    login: (username, password) => client.post('/login', { username, password }, { withCredentials: true }),
    verifyMFA: (challengeToken, code) => client.post('/auth/mfa-verifications', {
      challenge_token: challengeToken,
      code
    }, { withCredentials: true }),
    selectContext: (selectionTicket, context) => client.post('/auth/context-selections', {
      selection_ticket: selectionTicket,
      context_type: context.type,
      ...(context.tenant_membership_id
        ? { tenant_membership_id: context.tenant_membership_id }
        : {})
    }, { withCredentials: true }),
    refresh: () => client.post('/refresh', null, { withCredentials: true }),
    logout: () => client.post('/logout', null, { withCredentials: true }),
    getCurrentUser: () => client.get('/users/me'),
    getUser: (token) => client.get('/users/me', {
      headers: { Authorization: `Bearer ${token}` }
    }),
    getAuthContext: (token) => client.get('/auth/context', {
      headers: { Authorization: `Bearer ${token}` }
    })
  }
}

export function createAuthStore(storeName, authAPI, options = {}) {
  const { persistUser = true, extraGetters = {}, extraActions = {} } = options
  const baseConfig = createAuthStoreConfig(storeName, authAPI, { persistUser })
  return {
    ...baseConfig,
    getters: { ...baseConfig.getters, ...extraGetters },
    actions: { ...baseConfig.actions, ...extraActions }
  }
}

export function createAPIClient(getAuthStore, options = {}) {
  const {
    moduleName,
    baseURL = '/api/v1',
    timeout = 30_000,
    extractData = true,
    enableRefresh = true,
    onRefreshFailed = redirectToLogin
  } = options
  if (!moduleName) throw new Error('moduleName is required for createAPIClient')

  const client = axios.create({ baseURL, timeout })
  client.interceptors.request.use(createAuthInterceptor(getAuthStore, moduleName), Promise.reject)

  if (!enableRefresh) {
    if (extractData) client.interceptors.response.use((response) => response.data, Promise.reject)
    return client
  }

  const [onFulfilled, onRejected] = createRefreshInterceptor(getAuthStore, {
    axiosInstance: client,
    onRefreshFailed
  })
  client.interceptors.response.use(
    (response) => extractData ? onFulfilled(response).data : onFulfilled(response),
    async (error) => {
      return onRejected(error)
    }
  )
  return client
}
