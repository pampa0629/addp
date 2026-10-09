import { browserTestOrigin } from '../../../common-frontend/basic/src/utils/browserTestIsolation.mjs'
import { expect, test } from '@playwright/test'
import { isAnonymousRefreshConsoleError, isAnonymousRefreshResponse } from './online/transfer-browser-support.js'

test('only the native anonymous refresh 401 is an expected initialization diagnostic', async ({ page }) => {
  const messages = []
  const responses = []
  page.on('console', message => {
    if (message.type() === 'error') messages.push(message)
  })
  page.on('response', response => {
    if (new URL(response.url()).pathname.startsWith('/api/v1/system/')) responses.push(response)
  })
  await page.route('**/api/v1/system/refresh*', route => route.fulfill({
    status: Number(new URL(route.request().url()).searchParams.get('status') || 401),
    json: { error: 'authentication_required' }
  }))
  await page.route('**/api/v1/system/auth/context', route => route.fulfill({ status: 401, json: { error: 'authentication_required' } }))
  await page.goto('/e2e/fixtures/auth-fixture.html?role=health')
  await page.evaluate(async () => {
    await fetch('/api/v1/system/refresh', { method: 'POST' })
    await fetch('/api/v1/system/auth/context')
  })
  await expect.poll(() => messages.length).toBe(2)
  const refresh = messages.find(message => new URL(message.location().url).pathname === '/api/v1/system/refresh')
  const context = messages.find(message => new URL(message.location().url).pathname === '/api/v1/system/auth/context')
  expect(isAnonymousRefreshConsoleError(refresh, false)).toBe(true)
  expect(isAnonymousRefreshConsoleError(refresh, true)).toBe(false)
  expect(isAnonymousRefreshConsoleError(context, false)).toBe(false)
  await page.evaluate(async () => {
    await fetch('/api/v1/system/refresh')
    await fetch('/api/v1/system/refresh?status=403', { method: 'POST' })
    await fetch('/api/v1/system/refresh?status=500', { method: 'POST' })
  })
  await expect.poll(() => responses.length).toBe(5)
  expect(responses.map(response => isAnonymousRefreshResponse(response, false)))
    .toEqual([true, false, false, false, false])
  expect(responses.map(response => isAnonymousRefreshResponse(response, true)))
    .toEqual([false, false, false, false, false])
})

test('direct module ports redirect before refresh and all top-level pages share strict cookie rotation', async ({ context, page }) => {
  let generation = 0
  let refreshes = 0
  let logouts = 0
  let reuseDetected = false
  let releaseRefresh
  const concurrentRefresh = new Promise(resolve => { releaseRefresh = resolve })
  const refreshOrigins = []
  await context.addCookies([{ name: 'fixture_refresh', value: 'refresh-0', url: browserTestOrigin('console'), httpOnly: true, sameSite: 'Lax' }])
  await context.route('**/e2e/auth-api/**', async route => {
    if (new URL(route.request().url()).pathname.endsWith('/logout')) {
      logouts++
      const bearer = await route.request().headerValue('authorization')
      const cookie = await route.request().headerValue('cookie')
      if (bearer !== `Bearer access-${generation}` || !cookie?.split('; ').includes(`fixture_refresh=refresh-${generation}`)) {
        await route.fulfill({ status: 401, json: { message: 'authentication_required' } })
        return
      }
      await route.fulfill({ status: 204, headers: { 'set-cookie': 'fixture_refresh=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax' } })
      return
    }
    refreshes++
    refreshOrigins.push(new URL(route.request().url()).origin)
    const cookie = await route.request().headerValue('cookie')
    if (!cookie?.split('; ').includes(`fixture_refresh=refresh-${generation}`)) {
      reuseDetected = true
      await route.fulfill({ status: 401, json: { message: 'refresh_token_reuse_detected' } })
      return
    }
    generation++
    // Release only after both real page handlers have entered refresh, avoiding
    // assumptions about background-tab actionability or machine load.
    if (generation === 2) await concurrentRefresh
    await route.fulfill({ json: { access_token: `access-${generation}`, expires_in: 300 },
      headers: { 'set-cookie': `fixture_refresh=refresh-${generation}; Path=/; HttpOnly; SameSite=Lax` } })
  })
  await page.goto('/e2e/fixtures/auth-fixture.html?role=shared-top-level&iframe=1')
  await expect(page.getByTestId('status')).toHaveText('authenticated')
  const embedded = page.frameLocator('iframe[title="shared-module-auth"]')
  await expect(embedded.getByTestId('token')).toHaveText('access-1')

  const independent = await context.newPage()
  const initialRefreshes = refreshes
  await independent.goto(`${browserTestOrigin('system')}/module-ui/system/e2e/fixtures/browser-session.html?tab=details#section`)
  await expect(independent).toHaveURL(`${browserTestOrigin('console')}/module-ui/system/e2e/fixtures/browser-session.html?tab=details#section`)
  await expect(independent.getByTestId('status')).toHaveText('authenticated')
  await expect(independent.getByTestId('token')).toHaveText('access-1')
  expect(refreshes).toBe(initialRefreshes)

  await Promise.all([page.getByTestId('refresh').click(), independent.getByTestId('refresh').click()])
  await expect(page.getByTestId('status')).toHaveText('refreshing')
  await expect(independent.getByTestId('status')).toHaveText('refreshing')
  releaseRefresh()
  await expect(page.getByTestId('token')).toHaveText('access-2')
  await expect(independent.getByTestId('token')).toHaveText('access-2')
  await expect(embedded.getByTestId('token')).toHaveText('access-2')
  expect(refreshes).toBe(2)
  expect(reuseDetected).toBe(false)
  expect(new Set(refreshOrigins)).toEqual(new Set([browserTestOrigin('console')]))
  await independent.reload()
  await expect(independent.getByTestId('token')).toHaveText('access-2')
  expect(refreshes).toBe(2)
  await independent.getByTestId('logout').click()
  await expect(page.getByTestId('token')).toBeEmpty()
  await expect(independent.getByTestId('token')).toBeEmpty()
  await expect(embedded.getByTestId('token')).toBeEmpty()
  expect(logouts).toBe(1)
  expect((await context.cookies()).some(cookie => cookie.name === 'fixture_refresh')).toBe(false)
})

test('resource picker and module 401s share one parent refresh in an iframe', async ({ page }) => {
  const requests = []
  await page.route('**/e2e/resource-api/**', async route => {
    const token = await route.request().headerValue('authorization')
    requests.push({ path: new URL(route.request().url()).pathname, token })
    const fresh = token === 'Bearer resource-fresh-token'
    await route.fulfill({ status: fresh ? 200 : 401, contentType: 'application/json',
      body: JSON.stringify(fresh ? { data: [] } : { error: 'expired' }) })
  })
  await page.goto('/e2e/fixtures/auth-fixture.html?role=resource-parent')
  const embedded = page.frameLocator('iframe[title="embedded-auth-client"]')
  await expect(embedded.getByTestId('status')).toHaveText('resources-loaded')
  await expect(embedded.getByTestId('token')).toHaveText('resource-fresh-token')
  await expect(page.getByTestId('request-count')).toHaveText('1')
  expect(requests).toHaveLength(8)
  for (const path of new Set(requests.map(request => request.path))) {
    expect(requests.filter(request => request.path === path).map(request => request.token))
      .toEqual(['Bearer resource-expired-token', 'Bearer resource-fresh-token'])
  }
})

test('embedded client retries until the delayed Console coordinator is ready', async ({ page }) => {
  await page.goto('/e2e/fixtures/auth-fixture.html?role=parent')

  const embedded = page.frameLocator('iframe[title="embedded-auth-client"]')
  await expect(embedded.getByTestId('status')).toHaveText('authenticated')
  await expect(embedded.getByTestId('token')).toHaveText('parent-access-token')

  await expect.poll(async () => Number(await page.getByTestId('request-count').textContent())).toBeGreaterThan(1)
  const requestIDs = (await page.getByTestId('request-ids').textContent()).split(',').filter(Boolean)
  expect(new Set(requestIDs).size).toBe(1)
})

test('new tab replaces a revoked peer token before completing session initialization', async ({ context, page }) => {
  let refreshRequests = 0
  const identityTokens = []

  await context.route('**/e2e/auth-api/**', async (route) => {
    const request = route.request()
    const pathname = new URL(request.url()).pathname
    if (pathname.endsWith('/refresh')) {
      refreshRequests += 1
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ access_token: 'fresh-access-token', expires_in: 300 })
      })
      return
    }

    const accessToken = (await request.headerValue('authorization'))?.replace(/^Bearer\s+/i, '') || ''
    identityTokens.push(accessToken)
    if (accessToken !== 'fresh-access-token') {
      await route.fulfill({
        status: 401,
        contentType: 'application/json',
        body: JSON.stringify({ message: 'token_revoked' })
      })
      return
    }

    const body = pathname.endsWith('/users/me')
      ? { id: 1, username: 'e2e-user' }
      : { context: { type: 'tenant' }, authorization: { role_assignments: [] } }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
  })

  await page.goto('/e2e/fixtures/auth-fixture.html?role=peer')
  await expect(page.getByTestId('status')).toHaveText('peer-ready')
  await expect(page.getByTestId('token')).toHaveText('peer-revoked-token')

  const newTab = await context.newPage()
  await newTab.goto('/e2e/fixtures/auth-fixture.html?role=recovery')
  await expect(newTab.getByTestId('status')).toHaveText('authenticated')
  await expect(newTab.getByTestId('token')).toHaveText('fresh-access-token')

  expect(identityTokens).toContain('peer-revoked-token')
  expect(identityTokens).toContain('fresh-access-token')
  expect(refreshRequests).toBe(1)
})
