/**
 * 通用认证 Composable
 *
 * 提供统一的认证逻辑,供所有模块复用:
 * - 路由守卫标准实现
 * - Promise 链用户加载管理
 * - iframe 检测和 query token 处理
 */

import axios from 'axios'

/**
 * 标准路由守卫工厂函数
 *
 * @param {Function|Object} authStoreOrGetter - Pinia auth store 实例或 getter 函数
 * @param {Object} config - 配置选项
 * @param {string} config.moduleName - 模块名称 (用于日志和重定向)
 * @param {string} config.loginRouteName - 登录路由名称 (默认: 'Login')
 * @param {Function} config.normalizeRedirect - 重定向路径规范化函数 (可选)
 * @returns {Function} Vue Router beforeEach 守卫函数
 *
 * @example
 * // manager/frontend/src/router/index.js
 * import { createAuthGuard } from '@common-ui'
 * import { useAuthStore } from '../store/auth'
 *
 * router.beforeEach(createAuthGuard(useAuthStore, {
 *   moduleName: 'Manager',
 *   loginRouteName: 'Login'
 * }))
 */
export function createAuthGuard(authStoreOrGetter, config = {}) {
  const {
    moduleName = 'Module',
    loginRouteName = 'Login',
    normalizeRedirect = (path) => path
  } = config

  return async (to, from, next) => {
    // 支持传入函数或直接传入 store 实例
    const authStore = typeof authStoreOrGetter === 'function' ? authStoreOrGetter() : authStoreOrGetter
    const queryToken = typeof to.query.token === 'string' ? to.query.token : null

    // 0️⃣ 如果正在加载用户信息,等待完成后再放行
    if (authStore.isLoadingUser) {
      console.log(`[${moduleName} Router] User is loading, waiting for completion...`)
      try {
        await authStore.waitForUserLoad()
        console.log(`[${moduleName} Router] User loaded successfully`)
      } catch (error) {
        console.error(`[${moduleName} Router] Failed to wait for user load:`, error)
        authStore.logout()
        return next({
          name: loginRouteName,
          query: { redirect: normalizeRedirect(to.fullPath) }
        })
      }
      // 🆕 等待完成后继续执行后续检查，不要直接 next()
      // return next() // ❌ 旧代码：直接放行
    }

    // 1️⃣ 处理 Console iframe 传递的 token
    if (queryToken) {
      console.log(`[${moduleName} Router] Processing query token`)
      authStore.setToken(queryToken)

      try {
        await authStore.fetchUser()
      } catch (error) {
        console.error(`[${moduleName} Router] Failed to fetch user:`, error)
        authStore.logout()
        return next({
          name: loginRouteName,
          query: { redirect: normalizeRedirect(to.fullPath) }
        })
      }

      // 去掉 URL 中的 token 参数 (避免泄露)
      const { token: _removed, ...restQuery } = to.query
      next({ path: to.path, query: restQuery, replace: true })
      return
    }

    // 2️⃣ 已认证但无用户信息 (刷新页面后)
    if (authStore.isAuthenticated && !authStore.user) {
      console.log(`[${moduleName} Router] User authenticated but no user info, fetching...`)
      try {
        await authStore.fetchUser()
      } catch (error) {
        console.error(`[${moduleName} Router] Failed to refresh user:`, error)
        authStore.logout()
        return next({
          name: loginRouteName,
          query: { redirect: normalizeRedirect(to.fullPath) }
        })
      }
    }

    // 3️⃣ iframe 环境检测（仅用于日志，不跳过认证检查）
    const isInIframe = window.self !== window.top
    if (isInIframe) {
      console.log(`[${moduleName} Router] Running in iframe, token from Console:`, !!authStore.token)
    }

    // 4️⃣ 标准路由守卫 (检查是否需要认证)
    const requiresAuth = to.matched.some(record => record.meta?.requiresAuth !== false)
    const isPublic = to.name === loginRouteName

    // 🆕 已登录用户访问登录页，重定向到首页
    if (authStore.isAuthenticated && isPublic) {
      console.log(`[${moduleName} Router] Already authenticated, redirecting from login page`)
      return next('/')
    }

    if (!authStore.isAuthenticated && requiresAuth && !isPublic) {
      return next({
        name: loginRouteName,
        query: { redirect: normalizeRedirect(to.fullPath) }
      })
    }

    next()
  }
}

/**
 * 创建智能等待 Axios 请求拦截器
 *
 * @param {Function|Object} authStoreOrGetter - Pinia auth store 实例或 getter 函数
 * @param {string} moduleName - 模块名称 (用于日志)
 * @returns {Function} Axios request interceptor
 *
 * @example
 * // manager/frontend/src/api/client.js
 * import { createAuthInterceptor } from '@common-ui'
 * import { useAuthStore } from '../store/auth'
 *
 * client.interceptors.request.use(
 *   createAuthInterceptor(() => useAuthStore(), 'Manager')  // 传入函数，推迟调用
 * )
 */
export function createAuthInterceptor(authStoreOrGetter, moduleName = 'Module') {
  return async (config) => {
    // 支持传入函数（懒加载）或直接传入 store 实例
    const authStore = typeof authStoreOrGetter === 'function'
      ? authStoreOrGetter()
      : authStoreOrGetter

    // 🔧 移除等待逻辑以避免死锁
    // 原因: 路由守卫已经负责等待用户加载，这里等待会导致:
    // fetchUser() 调用 API → 拦截器等待 fetchUser() → 死锁

    // 添加 Authorization 头
    const token = authStore.token || localStorage.getItem('token')
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }

    return config
  }
}

/**
 * 标准 Auth Store 工厂函数 (Pinia defineStore 配置)
 *
 * @internal
 * ⚠️ 内部函数，仅供 createAuthStore 使用，不应直接调用
 *
 * 所有模块应使用 createAuthStore() 而不是此函数，以避免 getter 覆盖 bug。
 * 此函数已从公共 API 中移除，但保留用于内部实现。
 *
 * @param {string} storeName - Store 名称 (建议: '{module}-auth')
 * @param {Object} authAPI - 认证 API 对象
 * @param {Function} authAPI.getUser - 获取用户信息的 API 方法
 * @param {Object} options - 可选配置
 * @param {boolean} options.persistUser - 是否持久化 user 对象到 localStorage (默认: true)
 * @returns {Object} Pinia store 配置对象
 */
function createAuthStoreConfig(storeName, authAPI, options = {}) {
  const { persistUser = true } = options

  return {
    state: () => ({
      token: localStorage.getItem('token') || null,
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
      userLoadPromise: null
    }),

    getters: {
      isAuthenticated: (state) => !!state.token
    },

    actions: {
      async login(username, password) {
        const response = await authAPI.login(username, password)
        const accessToken = response.data?.access_token

        if (!accessToken) {
          throw new Error('登录响应缺少访问令牌')
        }

        this.setToken(accessToken)
        await this.fetchUser()
      },

      setToken(token) {
        this.token = token
        if (token) {
          localStorage.setItem('token', token)
        } else {
          localStorage.removeItem('token')
        }
      },

      async fetchUser() {
        // 如果已有请求在进行中,返回该 Promise (避免重复请求)
        if (this.userLoadPromise) {
          console.log(`[${storeName}] fetchUser already in progress, returning existing promise`)
          return this.userLoadPromise
        }

        if (!this.token) {
          this.user = null
          if (persistUser) {
            localStorage.removeItem('user')
          }
          return
        }

        console.log(`[${storeName}] Starting fetchUser`)
        this.isLoadingUser = true

        // 创建 Promise 链
        this.userLoadPromise = authAPI.getUser(this.token)
          .then(response => {
            this.user = response.data
            if (persistUser) {
              localStorage.setItem('user', JSON.stringify(this.user))
            }
            console.log(`[${storeName}] fetchUser completed successfully`)
            return response
          })
          .catch(error => {
            console.error(`[${storeName}] fetchUser failed:`, error)
            throw error
          })
          .finally(() => {
            this.isLoadingUser = false
            this.userLoadPromise = null
          })

        return this.userLoadPromise
      },

      async waitForUserLoad() {
        // 如果有正在进行的请求,等待其完成
        if (this.userLoadPromise) {
          console.log(`[${storeName}] Waiting for existing fetchUser to complete`)
          return this.userLoadPromise
        }

        // 如果 user 已加载,直接返回
        if (this.user) {
          console.log(`[${storeName}] User already loaded`)
          return Promise.resolve({ data: this.user })
        }

        // 既没有正在进行的请求,user 也未加载
        console.warn(`[${storeName}] waitForUserLoad called but no user load in progress`)
        throw new Error('No user load in progress')
      },

      logout() {
		if (typeof authAPI.logout === 'function') {
		  authAPI.logout().catch(error => {
			console.warn(`[${storeName}] remote logout failed:`, error)
		  })
		}
        this.setToken(null)
        this.user = null
        this.isLoadingUser = false
        this.userLoadPromise = null
        if (persistUser) {
          localStorage.removeItem('user')
        }
      }
    }
  }
}

/**
 * 创建自动刷新 Token 的 Axios 响应拦截器
 *
 * 当接收到 401 错误时,自动尝试刷新 token:
 * - 如果刷新成功,重试原请求
 * - 如果刷新失败或无 token,跳转登录页
 * - 使用队列机制避免多个并发请求同时刷新
 *
 * @param {Object} authStore - Pinia auth store 实例
 * @param {Object} config - 配置选项
 * @param {string} config.moduleName - 模块名称 (用于日志)
 * @param {string} config.systemBaseURL - System 服务地址 (用于调用刷新 API)
 * @param {Function} config.axiosInstance - 用于刷新成功后重试请求的 Axios 实例
 * @param {Function} config.onRefreshFailed - 刷新失败回调 (可选,默认跳转登录页)
 * @returns {Function} Axios response interceptor (success handler, error handler)
 *
 * @example
 * // manager/frontend/src/api/client.js
 * import { createRefreshInterceptor } from '@common-ui'
 * import { useAuthStore } from '../store/auth'
 *
 * const [onFulfilled, onRejected] = createRefreshInterceptor(useAuthStore(), {
 *   moduleName: 'Manager',
 *   systemBaseURL: 'http://localhost:8180',
 *   axiosInstance: client
 * })
 *
 * client.interceptors.response.use(onFulfilled, onRejected)
 */
function resolveAuthStore(authStoreOrGetter) {
  return typeof authStoreOrGetter === 'function'
    ? authStoreOrGetter()
    : authStoreOrGetter
}

function createTokenRefresher(authStoreOrGetter, config = {}) {
  const {
    moduleName = 'Module',
    systemBaseURL = '',
    fetch: fetchImpl = globalThis.fetch.bind(globalThis),
    onRefreshFailed = () => {
      // iframe 环境下，尝试通知父窗口重新登录
      const isInIframe = window.self !== window.top
      if (isInIframe) {
        console.log(`[${moduleName} RefreshInterceptor] In iframe, attempting to redirect parent window`)
        try {
          window.top.location.href = '/login'
        } catch (e) {
          // 跨域限制，无法访问父窗口，只能重定向自己
          console.warn(`[${moduleName} RefreshInterceptor] Cannot access parent window, redirecting iframe`)
          window.location.href = '/login'
        }
      } else {
        window.location.href = '/login'
      }
    }
  } = config

  let refreshPromise = null

  return async () => {
    if (refreshPromise) {
      console.log(`[${moduleName} Auth] Waiting for ongoing token refresh...`)
      return refreshPromise
    }

    refreshPromise = (async () => {
      const authStore = resolveAuthStore(authStoreOrGetter)
      const refreshURL = `${systemBaseURL.replace(/\/$/, '')}/api/v1/system/refresh`
      const response = await fetchImpl(refreshURL, {
        method: 'POST',
		credentials: 'include',
        headers: {
		  'Content-Type': 'application/json'
        }
      })

      if (!response.ok) {
        throw new Error(`Refresh failed with status ${response.status}`)
      }

      const data = await response.json()
      const newToken = data.access_token

      if (!newToken) {
        throw new Error('No access_token in refresh response')
      }

      authStore.setToken(newToken)
      console.log(`[${moduleName} Auth] Token refreshed successfully`)
      return newToken
    })()

    try {
      return await refreshPromise
    } catch (refreshError) {
      console.error(`[${moduleName} Auth] Token refresh failed:`, refreshError)
      const authStore = resolveAuthStore(authStoreOrGetter)
      authStore.logout()
      onRefreshFailed()
      throw refreshError
    } finally {
      refreshPromise = null
    }
  }
}

/**
 * 创建带 ADDP 认证和 Token 自动刷新的 Fetch。
 *
 * 用于 SSE、流式上传等不能通过 Axios 发起的请求。认证行为与
 * createAPIClient 保持一致：动态读取当前 Token、401 后刷新并只重试一次。
 */
export function createAuthenticatedFetch(authStoreOrGetter, config = {}) {
  const {
    moduleName,
    systemBaseURL = '',
    fetch: fetchImpl = globalThis.fetch.bind(globalThis),
    onRefreshFailed
  } = config

  if (!moduleName) {
    throw new Error('moduleName is required for createAuthenticatedFetch')
  }

  const refreshToken = createTokenRefresher(authStoreOrGetter, {
    moduleName,
    systemBaseURL,
    fetch: fetchImpl,
    ...(onRefreshFailed ? { onRefreshFailed } : {})
  })

  return async (input, init = {}) => {
    const isRequest = typeof Request !== 'undefined' && input instanceof Request
    const baseHeaders = new Headers(isRequest ? input.headers : undefined)
    new Headers(init.headers).forEach((value, key) => baseHeaders.set(key, value))

    const requestWithToken = (token) => {
      const headers = new Headers(baseHeaders)
      if (token) {
        headers.set('Authorization', `Bearer ${token}`)
      } else {
        headers.delete('Authorization')
      }
      return { ...init, headers }
    }

    const authStore = resolveAuthStore(authStoreOrGetter)
    const currentToken = authStore.token || localStorage.getItem('token')
    const retryInput = isRequest ? input.clone() : input
    const response = await fetchImpl(input, requestWithToken(currentToken))

    if (response.status !== 401) {
      return response
    }

    const newToken = await refreshToken()
    return fetchImpl(retryInput, requestWithToken(newToken))
  }
}

export function createRefreshInterceptor(authStoreOrGetter, config = {}) {
  const {
    moduleName = 'Module',
    systemBaseURL = '',
    fetch: fetchImpl,
    axiosInstance,
    onRefreshFailed = () => {
      const isInIframe = window.self !== window.top
      if (isInIframe) {
        try {
          window.top.location.href = '/login'
        } catch {
          window.location.href = '/login'
        }
      } else {
        window.location.href = '/login'
      }
    }
  } = config

  const refreshToken = createTokenRefresher(authStoreOrGetter, {
    moduleName,
    systemBaseURL,
    ...(fetchImpl ? { fetch: fetchImpl } : {}),
    onRefreshFailed
  })

  // 成功响应处理器 (直接透传)
  const onFulfilled = (response) => response

  // 错误响应处理器 (处理 401 并自动刷新)
  const onRejected = async (error) => {
    // 支持传入函数（懒加载）或直接传入 store 实例
    const authStore = resolveAuthStore(authStoreOrGetter)

    const originalRequest = error.config

    // 非 401 错误,直接拒绝
    if (error.response?.status !== 401) {
      return Promise.reject(error)
    }

    // 已经重试过,不再重试
    if (originalRequest._retry) {
      console.error(`[${moduleName} RefreshInterceptor] Refresh failed, redirecting to login`)
      authStore.logout()
      onRefreshFailed()
      return Promise.reject(error)
    }

    originalRequest._retry = true

    console.log(`[${moduleName} RefreshInterceptor] Starting token refresh...`)

    try {
      const newToken = await refreshToken()
      originalRequest.headers.Authorization = `Bearer ${newToken}`
      return axiosInstance(originalRequest)
    } catch (refreshError) {
      return Promise.reject(refreshError)
    }
  }

  return [onFulfilled, onRejected]
}

/**
 * 创建认证 Store 工厂函数（推荐使用）
 *
 * 安全地创建认证 store，避免 getter 覆盖 bug
 *
 * @param {string} storeName - Store 名称（建议: '{module}-auth'）
 * @param {Object} authAPI - 认证 API 对象
 * @param {Object} options - 配置选项
 * @param {boolean} options.persistUser - 是否持久化用户信息（默认: true）
 * @param {Object} options.extraGetters - 额外的 getters（可选）
 * @param {Object} options.extraActions - 额外的 actions（可选）
 * @returns {Object} Store 配置对象
 *
 * @example
 * // 标准用法
 * export const useAuthStore = defineStore('meta-auth',
 *   createAuthStore('meta-auth', authAPI, {
 *     persistUser: true
 *   })
 * )
 *
 * @example
 * // 添加额外的 getters
 * export const useAuthStore = defineStore('develop-auth',
 *   createAuthStore('develop-auth', authAPI, {
 *     persistUser: true,
 *     extraGetters: {
 *       username: (state) => state.user?.username || ''
 *     }
 *   })
 * )
 *
 * @example
 * // 添加额外的 actions（高级用法）
 * export const useAuthStore = defineStore('custom-auth',
 *   createAuthStore('custom-auth', authAPI, {
 *     persistUser: true,
 *     extraActions: {
 *       async customAction() {
 *         // 自定义逻辑
 *       }
 *     }
 *   })
 * )
 */
export function createAuthStore(storeName, authAPI, options = {}) {
  const { persistUser = true, extraGetters = {}, extraActions = {} } = options

  const baseConfig = createAuthStoreConfig(storeName, authAPI, { persistUser })

  return {
    ...baseConfig,
    getters: {
      ...baseConfig.getters,
      ...extraGetters
    },
    actions: {
      ...baseConfig.actions,
      ...extraActions
    }
  }
}

/**
 * 创建认证 API 工厂函数
 *
 * @param {Object} client - Axios 客户端实例
 * @param {Object} options - 配置选项
 * @param {boolean} options.includeRegister - 是否包含注册功能
 * @returns {Object} 认证 API 对象
 */
export function createAuthAPI(client, options = {}) {
  const { includeRegister = false } = options

  const baseAPI = {
    login: (username, password) => {
	  return client.post('/login', { username, password }, { withCredentials: true })
    },

	logout: () => client.post('/logout', null, { withCredentials: true }),

    getCurrentUser: () => {
      return client.get('/users/me')
    },

    // 供 createAuthStoreConfig 使用
    getUser: (token) => {
      return client.get('/users/me', {
        headers: { Authorization: `Bearer ${token}` }
      })
    }
  }

  // 如果需要注册功能，添加 register 方法
  if (includeRegister) {
    baseAPI.register = (data) => {
      return client.post('/register', data)
    }
  }

  return baseAPI
}

/**
 * 创建 API 客户端工厂函数
 *
 * @param {Function} getAuthStore - 获取 auth store 的函数
 * @param {Object} options - 配置选项
 * @returns {Object} 配置好的 Axios 客户端
 */
export function createAPIClient(getAuthStore, options = {}) {
  const {
    moduleName,
    baseURL = '/api/v1',
    timeout = 30000,
    extractData = true,
    enableRefresh = true,
    systemBaseURL = import.meta.env?.DEV
      ? 'http://localhost:8180'
      : `${typeof window !== 'undefined' ? window.location.protocol : 'http:'}//${typeof window !== 'undefined' ? window.location.hostname : 'localhost'}:8180`,
    onRefreshFailed = () => {
      if (typeof localStorage !== 'undefined') {
        localStorage.removeItem('token')
      }
      if (typeof window !== 'undefined') {
        // 开发环境: 直接跳转 /login (Vue Router 会处理)
        // 生产环境: 跳转 /{module}/login (iframe 嵌入模式)
        const isDev = import.meta.env?.DEV
        const loginPath = isDev ? '/login' : `/${moduleName?.toLowerCase() || ''}/login`.replace('//', '/')
        window.location.href = loginPath
      }
    }
  } = options

  if (!moduleName) {
    throw new Error('moduleName is required for createAPIClient')
  }

  const client = axios.create({
    baseURL,
    timeout
  })

  // 1. 请求拦截器 - 添加 JWT token
  client.interceptors.request.use(
    createAuthInterceptor(getAuthStore, moduleName),
    (error) => Promise.reject(error)
  )

  if (enableRefresh) {
    // 3. 添加 Token 刷新拦截器
    const [refreshOnFulfilled, refreshOnRejected] = createRefreshInterceptor(getAuthStore, {
      moduleName,
      systemBaseURL,
      axiosInstance: client,
      onRefreshFailed
    })

    // 4. 响应拦截器
    client.interceptors.response.use(
      (response) => {
        const processedResponse = refreshOnFulfilled(response)
        return extractData ? processedResponse.data : processedResponse
      },
      async (error) => {
        try {
          const recovered = await refreshOnRejected(error)
          return extractData ? recovered.data : recovered
        } catch (finalError) {
          // 401 错误日志
          if (finalError.response?.status === 401) {
            console.error(`[${moduleName} Client] 401 错误:`, {
              url: finalError.config?.url,
              method: finalError.config?.method
            })
          }
          return Promise.reject(finalError)
        }
      }
    )
  } else {
    // 不启用刷新时，只提取 data
    if (extractData) {
      client.interceptors.response.use(
        (response) => response.data,
        (error) => Promise.reject(error)
      )
    }
  }

  return client
}
