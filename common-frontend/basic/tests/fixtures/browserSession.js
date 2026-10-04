import {
  createBrowserAuthSession, createIframeAuthCoordinator, getAccessToken,
  getAccessTokenExpiresAt, subscribeAccessToken
} from '../../src/auth/authSession.js'

// Real BrowserAuthSession, with a deterministic HTTP backend supplied by Playwright.
document.body.innerHTML = `<output data-testid="status">initializing</output>
  <output data-testid="token"></output><output data-testid="error"></output>
  <button data-testid="refresh">Refresh</button><button data-testid="logout">Logout</button>
  <div id="frame-host"></div>`
const status = document.querySelector('[data-testid="status"]')
const token = document.querySelector('[data-testid="token"]')
const error = document.querySelector('[data-testid="error"]')
const request = async (path, accessToken) => {
  const response = await fetch(`/e2e/auth-api/${path}`, {
    method: 'POST', credentials: 'include',
    ...(accessToken ? { headers: { Authorization: `Bearer ${accessToken}` } } : {})
  })
  const payload = response.status === 204 ? null : await response.json()
  if (!response.ok) throw Object.assign(new Error(payload.message), { status: response.status })
  return payload
}
const session = createBrowserAuthSession({ refresh: () => request('refresh'), revoke: accessToken => request('logout', accessToken) })
subscribeAccessToken(snapshot => { token.textContent = snapshot.token || '' })
const showError = failure => { status.textContent = 'failed'; error.textContent = failure.message }
document.querySelector('[data-testid="refresh"]').onclick = async () => {
  status.textContent = 'refreshing'
  try {
    await session.refreshAccessToken({ force: true })
    status.textContent = 'authenticated'
  } catch (failure) { showError(failure) }
}
document.querySelector('[data-testid="logout"]').onclick = () => session.logout().catch(showError)
try {
  await session.initialize()
  status.textContent = 'authenticated'
  token.textContent = getAccessToken()
  if (window.self === window.top && new URL(location.href).searchParams.has('iframe')) {
    createIframeAuthCoordinator({
      allowedOrigins: [location.origin], getToken: getAccessToken, getExpiresAt: getAccessTokenExpiresAt,
      refreshToken: () => session.refreshAccessToken({ force: true }), logout: () => session.logout()
    })
    const iframe = document.createElement('iframe')
    iframe.title = 'shared-module-auth'
    iframe.src = '/module-ui/system/e2e/fixtures/browser-session.html'
    document.querySelector('#frame-host').appendChild(iframe)
  }
} catch (failure) { showError(failure) }
