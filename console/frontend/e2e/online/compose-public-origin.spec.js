import { expect, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { login } from './transfer-browser-support.js'

function required(name) {
  if (!process.env[name]) throw new Error(`missing Online environment: ${name}`)
  return process.env[name]
}

// Inspect the mounted production store without adding product debug hooks.
// Only a one-way fingerprint leaves browser memory, never an Access Token.
async function sessionProbe({ storeID, action = 'snapshot' }) {
  const app = document.querySelector('#app')?.__vue_app__
  const provides = app?._context.provides || {}
  const pinia = Reflect.ownKeys(provides).map(key => provides[key])
    .find(value => value?._s instanceof Map && value._s.has(storeID))
  const store = pinia?._s.get(storeID)
  if (!store) throw new Error('mounted_auth_store_missing')
  if (action === 'hold-lock') {
    if (!navigator.locks) throw new Error('browser_refresh_lock_unavailable')
    await new Promise(ready => {
      navigator.locks.request('addp-auth-refresh', async () => {
        await new Promise(release => {
          window.__onlineReleaseRefreshLock = release
          ready()
        })
      })
    })
    return true
  }
  if (action === 'queue-refresh') {
    window.__onlineRefreshResult = store.refreshAccessToken({ force: true }).then(() => true, () => false)
    return true
  }
  if (action === 'release-lock') {
    window.__onlineReleaseRefreshLock()
    delete window.__onlineReleaseRefreshLock
    return true
  }
  if (action === 'finish-refresh') {
    const succeeded = await window.__onlineRefreshResult
    delete window.__onlineRefreshResult
    return succeeded
  }
  const digest = store.token
    ? Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(store.token))))
      .map(value => value.toString(16).padStart(2, '0')).join('')
    : ''
  return {
    authenticated: store.sessionStatus === 'authenticated' && Boolean(store.token),
    fingerprint: digest,
    tenantID: String(store.authContext?.context?.tenant_id || ''),
    principalID: String(store.authContext?.principal?.id || ''),
    username: store.user?.username || '',
    tokenPersisted: Boolean(localStorage.getItem('token') || sessionStorage.getItem('token'))
  }
}

test('production Nginx shares one rotating browser session across Console, iframe and independent module', async ({ context, page }) => {
  const origin = required('ADDP_ONLINE_PUBLIC_ORIGIN')
  const tenantID = required('ADDP_ONLINE_TEST_TENANT_ID')
  const username = required('ADDP_ONLINE_READ_USER_USERNAME')
  const requests = [], responses = []
  context.on('request', request => {
    const url = new URL(request.url())
    if (url.pathname === '/api/v1/system/refresh') requests.push({ origin: url.origin, method: request.method() })
  })
  context.on('response', response => {
    const url = new URL(response.url())
    if (url.pathname === '/api/v1/system/refresh') responses.push(response.status())
  })
  await login(page, username, required('ADDP_ONLINE_READ_USER_PASSWORD'), '/manager/data-explorer')
  await expect(page.locator('iframe[data-testid="module-iframe"]')).toHaveAttribute('src', `${origin}/module-ui/manager/data-explorer`)
  const moduleFrame = () => page.frames().find(frame => frame.url().startsWith(`${origin}/module-ui/manager/`))
  await expect(page.frameLocator('iframe[data-testid="module-iframe"]').locator('.content-only')).toBeVisible()
  await expect.poll(async () => Boolean(moduleFrame() && (await moduleFrame().evaluate(sessionProbe, { storeID: 'manager-auth' })).authenticated)).toBe(true)
  const initial = await page.evaluate(sessionProbe, { storeID: 'console-auth' })
  expect(initial.authenticated && initial.tenantID === tenantID && initial.username === username && !initial.tokenPersisted).toBe(true)
  expect(Boolean(initial.principalID)).toBe(true)
  const frameBeforeRefresh = moduleFrame()

  const initialRefreshes = requests.length
  const independent = await context.newPage()
  await independent.goto(`${origin}/module-ui/manager/data-explorer?tab=session#renewal`)
  await expect(independent.locator('.user-dropdown')).toContainText(username)
  await expect.poll(async () => {
    const session = await independent.evaluate(sessionProbe, { storeID: 'manager-auth' })
    return session.authenticated && session.fingerprint === initial.fingerprint && session.tenantID === tenantID &&
      session.principalID === initial.principalID && !session.tokenPersisted
  }).toBe(true)
  expect(requests.length === initialRefreshes).toBe(true)
  const cookieBefore = (await context.cookies()).find(cookie => cookie.name === 'addp_refresh_token')
  expect(Boolean(cookieBefore?.httpOnly && cookieBefore.path === '/api/v1/system')).toBe(true)

  // Both public store calls enter refresh before the held lock is released.
  // The subsequent HTTP exchange goes through real Nginx, Gateway and System.
  await page.evaluate(sessionProbe, { storeID: 'console-auth', action: 'hold-lock' })
  try {
    await Promise.all([
      page.evaluate(sessionProbe, { storeID: 'console-auth', action: 'queue-refresh' }),
      independent.evaluate(sessionProbe, { storeID: 'manager-auth', action: 'queue-refresh' })
    ])
    expect(requests.length === initialRefreshes).toBe(true)
  } finally {
    await page.evaluate(sessionProbe, { storeID: 'console-auth', action: 'release-lock' })
  }
  expect(await page.evaluate(sessionProbe, { storeID: 'console-auth', action: 'finish-refresh' })).toBe(true)
  expect(await independent.evaluate(sessionProbe, { storeID: 'manager-auth', action: 'finish-refresh' })).toBe(true)
  const renewed = await page.evaluate(sessionProbe, { storeID: 'console-auth' })
  expect(renewed.authenticated && renewed.fingerprint !== initial.fingerprint).toBe(true)
  await expect(page.frameLocator('iframe[data-testid="module-iframe"]').locator('.content-only')).toBeVisible()
  await expect.poll(async () => {
    const peers = await Promise.all([
      independent.evaluate(sessionProbe, { storeID: 'manager-auth' }),
      moduleFrame().evaluate(sessionProbe, { storeID: 'manager-auth' })
    ])
    return peers.every(session => session.authenticated && session.fingerprint === renewed.fingerprint &&
      session.tenantID === tenantID && session.principalID === initial.principalID && !session.tokenPersisted)
  }).toBe(true)
  expect(requests.length - initialRefreshes).toBe(1)
  expect(responses.slice(initialRefreshes)).toEqual([200])
  const iframePreservedOnRefresh = frameBeforeRefresh === moduleFrame()
  expect(requests.every(request => request.origin === origin && request.method === 'POST')).toBe(true)
  const cookieAfter = (await context.cookies()).find(cookie => cookie.name === 'addp_refresh_token')
  expect(Boolean(cookieAfter?.httpOnly && cookieAfter.value !== cookieBefore.value)).toBe(true)
  await page.screenshot({ path: resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), 'public-origin-console.png') })
  await independent.screenshot({ path: resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), 'public-origin-independent.png') })

  await independent.reload()
  await expect(independent.locator('.user-dropdown')).toContainText(username)
  await expect.poll(async () => (await independent.evaluate(sessionProbe, { storeID: 'manager-auth' })).fingerprint === renewed.fingerprint).toBe(true)
  expect(requests.length - initialRefreshes).toBe(1)
  const logout = independent.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/system/logout')
  await independent.locator('.user-dropdown').hover()
  await independent.getByRole('menuitem', { name: /退出登录|Logout|Log out/i }).click()
  expect((await logout).status()).toBe(200)
  await expect(page).toHaveURL(/\/login(?:\?|$)/)
  await expect(independent).toHaveURL(/\/module-ui\/manager\/login(?:\?|$)/)
  expect((await context.cookies()).some(cookie => cookie.name === 'addp_refresh_token')).toBe(false)
  expect((await page.evaluate(sessionProbe, { storeID: 'console-auth' })).fingerprint === '').toBe(true)
  expect((await independent.evaluate(sessionProbe, { storeID: 'manager-auth' })).fingerprint === '').toBe(true)

  writeFileSync(required('ADDP_ONLINE_PUBLIC_ORIGIN_BROWSER_REPORT'), JSON.stringify({
    run_id: required('ADDP_ONLINE_TEST_RUN_ID'), origin, tenant_id: tenantID, username,
    concurrent_refreshes: 1, iframe_converged: true, cookie_rotated: true,
    reload_without_refresh: true, logout_propagated: true, javascript_tokens_persisted: false,
    iframe_preserved_on_refresh: iframePreservedOnRefresh
  }))
})
