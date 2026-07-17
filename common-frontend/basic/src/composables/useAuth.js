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
        store.sessionStatus = 'anonymous'
        if (persistUser) localStorage.removeItem('user')
        if (wasAuthenticated && store.sessionInitialized && !authSession.isEmbedded() && typeof window !== 'undefined') {
          window.location.assign('/login')
        }
      }
    })
    store.token = getAccessToken()
    store.tokenExpiresAt = getAccessTokenExpiresAt()
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
      isLoadingUser: false,
      userLoadPromise: null,
      sessionInitialized: false,
      sessionInitPromise: null,
      sessionStatus: 'idle',
      sessionError: null
    }),

    getters: {
      isAuthenticated: (state) => Boolean(state.token)
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
              await this.fetchUser()
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
        if (!payload?.access_token) throw new Error('登录响应缺少访问令牌')
        authSession.acceptToken(payload.access_token, payload.expires_in)
        await this.fetchUser()
        this.sessionInitialized = true
        this.sessionStatus = 'authenticated'
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

      async waitForUserLoad() {
        if (this.userLoadPromise) return this.userLoadPromise
        if (this.user) return { data: this.user }
        throw new Error(`${storeName}: user is not loading`)
      },

      clearLocalSession() {
        bindStore(this)
        authSession.clearToken({ broadcastEvent: false })
        this.user = null
        this.isLoadingUser = false
        this.userLoadPromise = null
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

export function createAuthAPI(client, options = {}) {
  const { includeRegister = false } = options
  const api = {
    login: (username, password) => client.post('/login', { username, password }, { withCredentials: true }),
    refresh: () => client.post('/refresh', null, { withCredentials: true }),
    logout: () => client.post('/logout', null, { withCredentials: true }),
    getCurrentUser: () => client.get('/users/me'),
    getUser: (token) => client.get('/users/me', {
      headers: { Authorization: `Bearer ${token}` }
    })
  }
  if (includeRegister) {
    api.register = (userData) => client.post('/register', userData)
  }
  return api
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
    onRefreshFailed = () => {
      if (typeof window !== 'undefined') window.location.href = '/login'
    }
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
