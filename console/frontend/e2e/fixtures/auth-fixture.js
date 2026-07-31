import { createPinia, defineStore, setActivePinia } from 'pinia'
import {
  clearRuntimeAccessToken,
  createBrowserAuthSession,
  createIframeAuthCoordinator,
  setRuntimeAccessToken
} from '@common-ui/auth/authSession.js'
import { createAuthStore } from '@common-ui/composables/useAuth.js'

const role = new URL(window.location.href).searchParams.get('role') || 'health'
const status = document.querySelector('[data-testid="status"]')
const tokenOutput = document.querySelector('[data-testid="token"]')
const errorOutput = document.querySelector('[data-testid="error"]')

function showResult(nextStatus, token = '', error = '') {
  status.textContent = nextStatus
  tokenOutput.textContent = token
  errorOutput.textContent = error
}

if (role === 'parent') {
  const requestIDs = []
  window.addEventListener('message', (event) => {
    const message = event.data
    if (message?.protocol !== 'addp-auth/v1' || message.type !== 'addp-auth-ready') return
    requestIDs.push(message.requestId)
    document.querySelector('[data-testid="request-count"]').textContent = String(requestIDs.length)
    document.querySelector('[data-testid="request-ids"]').textContent = requestIDs.join(',')
  })

  const iframe = document.createElement('iframe')
  iframe.src = './auth-fixture.html?role=embedded'
  iframe.title = 'embedded-auth-client'
  document.querySelector('#frame-host').appendChild(iframe)

  setTimeout(() => {
    window.__authCoordinator = createIframeAuthCoordinator({
      allowedOrigins: [window.location.origin],
      getToken: () => 'parent-access-token',
      getExpiresAt: () => Date.now() + 300_000,
      refreshToken: async () => 'parent-access-token',
      logout: async () => {}
    })
    showResult('coordinator-ready')
  }, 650)
} else if (role === 'embedded') {
  const session = createBrowserAuthSession({
    refresh: async () => {
      throw new Error('embedded_refresh_must_use_parent')
    }
  })
  try {
    const token = await session.initialize()
    showResult('authenticated', token)
  } catch (error) {
    showResult('failed', '', error.message)
  }
} else if (role === 'peer' || role === 'recovery') {
  clearRuntimeAccessToken()
  if (role === 'peer') {
    setRuntimeAccessToken('peer-revoked-token', Date.now() + 300_000)
  }

  const requestJSON = async (path, options = {}) => {
    const response = await fetch(`/e2e/auth-api${path}`, options)
    const payload = await response.json()
    if (!response.ok) {
      const error = new Error(payload.message || 'request_failed')
      error.status = response.status
      error.response = { status: response.status }
      throw error
    }
    return payload
  }
  const withToken = (accessToken) => ({
    headers: { Authorization: `Bearer ${accessToken}` }
  })
  const authAPI = {
    refresh: () => requestJSON('/refresh', { method: 'POST' }),
    logout: async () => {},
    switchContext: async () => {
      throw new Error('context_switch_not_used')
    },
    getUser: (accessToken) => requestJSON('/users/me', withToken(accessToken)),
    getAuthContext: (accessToken) => requestJSON('/auth/context', withToken(accessToken))
  }

  setActivePinia(createPinia())
  const useFixtureAuthStore = defineStore(
    `auth-e2e-${role}`,
    createAuthStore(`auth-e2e-${role}`, authAPI, { persistUser: false })
  )
  const store = useFixtureAuthStore()
  store.bindAuthSession()

  if (role === 'peer') {
    showResult('peer-ready', store.token)
  } else {
    try {
      const accessToken = await store.initializeSession()
      showResult(store.sessionStatus, accessToken)
    } catch (error) {
      showResult('failed', store.token || '', error.message)
    }
  }
} else {
  showResult('ready')
}
