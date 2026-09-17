import { expect, test } from '@playwright/test'

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
