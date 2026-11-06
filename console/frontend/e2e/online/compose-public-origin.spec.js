import { expect, test } from '@playwright/test'
import { randomUUID } from 'node:crypto'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { localIdentityTOTP } from './local-identity-support.mjs'
import { login } from './transfer-browser-support.js'

// Capture evidence explicitly after login. Automatic failure artifacts can contain credentials.
test.use({ locale: 'zh-CN', screenshot: 'off', trace: 'off', video: 'off', actionTimeout: 15000 })

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
    username: store.user?.local_account?.username || '',
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
  expect(initial).toMatchObject({ authenticated: true, tenantID, username, tokenPersisted: false })
  expect(Boolean(initial.principalID)).toBe(true)
  const frameBeforeRefresh = moduleFrame()
  await frameBeforeRefresh.evaluate(() => { window.__onlineDraft = 'unsaved browser state' })

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
  expect(iframePreservedOnRefresh).toBe(true)
  expect(await moduleFrame().evaluate(() => window.__onlineDraft)).toBe('unsaved browser state')
  await moduleFrame().evaluate(() => { delete window.__onlineDraft })
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
  expect((await logout).status()).toBe(204)
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

test.describe('production public login', () => {
  for (const entry of ['workbench', 'portal']) {
    test(`${entry} requires TOTP and explicit tenant selection before restoring the public URL`, async ({ context, page }) => {
      const origin = required('ADDP_ONLINE_PUBLIC_ORIGIN')
      const tenantID = required('ADDP_ONLINE_TEST_TENANT_ID')
      const principalID = required('ADDP_ONLINE_PUBLIC_MFA_PRINCIPAL_ID')
      const membershipID = required('ADDP_ONLINE_PUBLIC_MFA_MEMBERSHIP_ID')
      const alternateID = required('ADDP_ONLINE_PUBLIC_MFA_ALTERNATE_MEMBERSHIP_ID')
      const published = process.env.ADDP_ONLINE_PUBLIC_ORIGIN_WORKBENCH_FIXTURE
        ? JSON.parse(process.env.ADDP_ONLINE_PUBLIC_ORIGIN_WORKBENCH_FIXTURE)
        : null
      const appID = published?.application_id ?? randomUUID()
      const workbench = {
        url: `/data-apps/${appID}?preset=monthly#summary`,
        login: '/module-ui/workbench/login',
        api: `/api/v1/workbench/data_applications/${appID}/runtime`,
        status: published ? 200 : 404,
      }
      const portal = {
        url: '/portal/home?source=public-origin#latest',
        login: '/portal/login',
        api: '/api/v1/portal/home',
        status: 200,
      }
      const target = entry === 'workbench' ? workbench : portal
      const other = entry === 'workbench' ? portal : workbench
      const system = suffix => '/api/v1/system/' + suffix
      const refreshCookie = async () => (await context.cookies(origin + system('refresh')))
        .find(cookie => cookie.name === 'addp_refresh_token')
      const primary = page.locator('.auth-login-primary')
      const responseFor = (tab, path, method = 'GET', documentPath) => tab.waitForResponse(response => {
        const url = new URL(response.url())
        const request = response.request()
        if (url.origin !== origin || url.pathname !== path || request.method() !== method) return false
        const referer = request.headers().referer
        return !documentPath || Boolean(referer && new URL(referer).pathname === documentPath)
      }, { timeout: 20000 })
      // Workbench replaces the document after login. Start reading the
      // response body as soon as headers arrive, before waiting for landing.
      const captureJSON = async response => {
        try {
          return { response, payload: await response.json() }
        } catch {
          return { response, payload: undefined }
        }
      }
      const queryFor = (tab, destination) => published && destination === workbench
        ? responseFor(tab, published.query_path, 'POST').then(captureJSON)
        : undefined
      let stage = 'anonymous redirect'
      const check = (label, assertion) => {
        stage = label
        return assertion()
      }
      const assertIdentity = (phase, { response, payload }) => {
        check(`${phase}: AuthContext status`, () => expect(response.status()).toBe(200))
        check(`${phase}: AuthContext response body`, () => expect(Boolean(payload)).toBe(true))
        check(`${phase}: selected principal`, () => expect(payload.principal).toMatchObject({ id: principalID, type: 'user' }))
        check(`${phase}: selected tenant and membership`, () => expect(payload.context).toMatchObject({
          type: 'tenant', tenant_id: tenantID, tenant_membership_id: membershipID,
        }))
        check(`${phase}: assurance and methods`, () => expect(payload.authentication).toMatchObject({
          assurance_level: 'aal2', methods: ['password', 'totp'],
        }))
      }
      const assertLanding = async (phase, tab, destination, response, query) => {
        await check(`${phase}: complete return URL`, () => expect(tab).toHaveURL(origin + destination.url))
        check(`${phase}: landing API status`, () => expect(response.status()).toBe(destination.status))
        if (destination === workbench && published) {
          const runtime = await check(`${phase}: Workbench published Runtime body`, () => response.json())
          check(`${phase}: Workbench published revision`, () =>
            expect(runtime).toMatchObject({ id: appID, revision_number: published.published_revision }))
          check(`${phase}: Workbench real Service query`, () => {
            expect(query.response.status()).toBe(200)
            expect(query.payload.data).toEqual(published.table_values.map(person_id => ({ person_id })))
          })
          const renderer = tab.getByTestId('data-application-runtime').getByTestId('renderer-host')
          for (const value of published.table_values) {
            await check(`${phase}: rendered Service value`, () =>
              expect(renderer.getByRole('cell', { name: value, exact: true })).toBeVisible())
          }
        } else if (destination === workbench) {
          await check(`${phase}: Workbench missing application message`, () =>
            expect(tab.getByTestId('data-application-runtime').getByRole('alert')).toContainText('数据应用不存在'))
        } else {
          await check(`${phase}: Portal empty fixture response`, async () =>
            expect((await response.json()).total_published).toBe(0))
          await check(`${phase}: Portal home heading`, () =>
            expect(tab.getByRole('heading', { name: '资产门户', exact: true })).toBeVisible())
          await check(`${phase}: Portal empty fixture message`, () =>
            expect(tab.getByText('暂无已上架资产', { exact: true })).toBeVisible())
        }
      }
      let businessRequests = 0
      let refreshRequests = 0
      const statuses = {}
      const observedPaths = {
        [system('login')]: 'password',
        [system('auth/mfa-verifications')]: 'mfa',
        [system('auth/context-selections')]: 'selection',
        [system('auth/context')]: 'identity',
        [system('refresh')]: 'refresh',
        [workbench.api]: 'workbench',
        [portal.api]: 'portal',
      }
      context.on('response', response => {
        const name = observedPaths[new URL(response.url()).pathname]
        if (name) statuses[name] = response.status()
      })
      context.on('request', request => {
        const url = new URL(request.url())
        if (url.pathname === system('refresh')) refreshRequests++
        if (url.pathname.startsWith('/api/') && !url.pathname.startsWith('/api/v1/system/')) businessRequests++
      })
      const assertAnonymous = async () => {
        await expect(page).toHaveURL(url => url.origin === origin && url.pathname === target.login && url.searchParams.get('redirect') === target.url)
        expect(Boolean(await refreshCookie())).toBe(false)
        expect(businessRequests).toBe(0)
      }

      try {
        await page.goto(target.url, { timeout: 20000 })
        await assertAnonymous()

        stage = 'password challenge'
        await page.locator('input[autocomplete=username]').fill(required('ADDP_ONLINE_PUBLIC_MFA_USERNAME'))
        await page.locator('input[autocomplete=current-password]').fill(required('ADDP_ONLINE_PUBLIC_MFA_PASSWORD'))
        const [passwordResponse] = await Promise.all([responseFor(page, system('login'), 'POST'), primary.click()])
        expect(passwordResponse.status()).toBe(200)
        const password = await passwordResponse.json()
        expect(password.next_action).toBe('verify_mfa')
        expect(password.mfa.method).toBe('totp')
        expect(Boolean(password.session)).toBe(false)
        const code = page.locator('input[autocomplete=one-time-code]')
        await expect(code).toBeVisible()
        await assertAnonymous()

        stage = 'TOTP and tenant choices'
        // Use a new TOTP counter after enrollment and the other browser case.
        await new Promise(done => setTimeout(done, 30000 - Date.now() % 30000 + 150))
        await code.fill(localIdentityTOTP(required('ADDP_ONLINE_PUBLIC_MFA_TOTP')))
        const [mfaResponse] = await Promise.all([responseFor(page, system('auth/mfa-verifications'), 'POST'), primary.click()])
        expect(mfaResponse.status()).toBe(200)
        const mfa = await mfaResponse.json()
        expect(mfa.next_action).toBe('select_context')
        expect(Boolean(mfa.session)).toBe(false)
        const choices = mfa.selection.contexts
        expect(choices.map(option => option.tenant_membership_id).sort()).toEqual([membershipID, alternateID].sort())
        expect(choices.every(option => option.type === 'tenant')).toBe(true)
        await expect(page.getByRole('radio')).toHaveCount(2)
        await expect(primary).toBeDisabled()
        await assertAnonymous()

        stage = 'explicit tenant and token issuance'
        const tenant = choices.find(option => option.tenant_membership_id === membershipID)
        await page.getByText(tenant.tenant_name, { exact: true }).click()
        await expect(page.getByRole('radio', { name: tenant.tenant_name, exact: true })).toBeChecked()
        // The Workbench login document also requests AuthContext before
        // leaving. Verify the runtime document's session after the return.
        const identityDocument = entry === 'workbench' ? new URL(target.url, origin).pathname : undefined
        const [{ response: selection, payload: session }, identity, landing, query] = await Promise.all([
          responseFor(page, system('auth/context-selections'), 'POST').then(captureJSON),
          responseFor(page, system('auth/context'), 'GET', identityDocument).then(captureJSON),
          responseFor(page, target.api),
          queryFor(page, target),
          primary.click(),
        ])
        check('selection status', () => expect(selection.status()).toBe(200))
        stage = 'submitted tenant choice'
        const choice = selection.request().postDataJSON()
        expect(choice.tenant_membership_id).toBe(membershipID)
        expect(choice.context_type).toBe('tenant')
        check('selection response body', () => expect(Boolean(session)).toBe(true))
        check('selection access token prefix', () => expect(Boolean(session.access_token?.startsWith('addp_at_'))).toBe(true))
        check('selection token type', () => expect(session.token_type).toBe('Bearer'))
        check('selection token expiry', () => expect(session.expires_in).toBeGreaterThan(0))

        assertIdentity('after login', identity)
        await assertLanding('after login', page, target, landing, query)
        stage = 'issued HttpOnly refresh cookie at System endpoint'
        const cookie = await refreshCookie()
        expect(Boolean(cookie?.httpOnly && cookie.path === '/api/v1/system')).toBe(true)

        stage = 'reload and rotate the session'
        const [refresh, reloadedIdentity, reloadedLanding, reloadedQuery] = await Promise.all([
          responseFor(page, system('refresh'), 'POST'),
          responseFor(page, system('auth/context')).then(captureJSON),
          responseFor(page, target.api),
          queryFor(page, target),
          page.reload({ timeout: 20000 }),
        ])
        check('reload refresh status', () => expect(refresh.status()).toBe(200))
        assertIdentity('after reload', reloadedIdentity)
        await assertLanding('after reload', page, target, reloadedLanding, reloadedQuery)
        stage = 'rotated HttpOnly refresh cookie at System endpoint'
        const renewedCookie = await refreshCookie()
        expect(Boolean(renewedCookie?.httpOnly && renewedCookie.value !== cookie.value)).toBe(true)

        stage = 'other module reuses the session'
        const before = refreshRequests
        const otherPage = await context.newPage()
        const [sharedIdentity, sharedLanding, sharedQuery] = await Promise.all([
          responseFor(otherPage, system('auth/context')).then(captureJSON),
          responseFor(otherPage, other.api),
          queryFor(otherPage, other),
          otherPage.goto(other.url, { timeout: 20000 }),
        ])
        assertIdentity('other module', sharedIdentity)
        await assertLanding('other module', otherPage, other, sharedLanding, sharedQuery)
        stage = 'other module uses the same bearer'
        const bearer = await reloadedLanding.request().headerValue('authorization')
        expect(Boolean(bearer?.startsWith('Bearer ') && bearer === await sharedLanding.request().headerValue('authorization'))).toBe(true)
        check('other module opens without another refresh', () => expect(refreshRequests).toBe(before))

        stage = 'write public login evidence'
        writeFileSync(resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), `public-origin-${entry}-login.json`), JSON.stringify({
          run_id: required('ADDP_ONLINE_TEST_RUN_ID'), entry, origin, tenant_id: tenantID,
          mfa: true, explicit_tenant_selection: true, full_return_url: true, http_only_refresh: true,
          reload_preserved_identity: true, cross_module_session_reused: true,
          workbench_runtime_status: workbench.status, portal_home_status: 200,
          ...(published ? {
            workbench_application_id: appID,
            workbench_published_revision: published.published_revision,
            workbench_table_values: published.table_values,
          } : {}),
        }))
      } catch {
        throw new Error(`${entry} public login failed at ${stage} (${JSON.stringify(statuses)})`)
      }
    })
  }
})
