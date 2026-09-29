import { expect, test } from '@playwright/test'

test('application-only account lands on its applications and cannot open asset home', async ({ page }) => {
  const businessRequests = []
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/v1/system/refresh') return route.fulfill({ json: { access_token: 'portal-application-token', expires_in: 300 } })
    if (path === '/api/v1/system/users/me') return route.fulfill({ json: { id: 32, username: 'portal-applications-user' } })
    if (path === '/api/v1/system/auth/context') return route.fulfill({ json: {
      context: { type: 'tenant', tenant_id: '3' },
      authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '3' }, permissions: ['asset.application.read'] }] }
    } })
    businessRequests.push(path)
    if (path === '/api/v1/portal/my/applications') return route.fulfill({ json: [] })
    return route.fulfill({ status: 403, json: { error: 'unexpected_business_request' } })
  })

  await page.goto('/portal/')
  await expect(page).toHaveURL(/\/portal\/my\/applications$/)
  await expect(page.getByRole('heading', { name: '我的申请与授权' })).toBeVisible()
  await page.goto('/portal/home')
  await expect(page).toHaveURL(/\/forbidden$/)
  expect(businessRequests).not.toContain('/api/v1/portal/home')
  await page.goto('/portal/login')
  await expect(page).toHaveURL(/\/portal\/my\/applications$/)
})

async function openAsset(page, permissions, { ownRating = null, accessStatus = null } = {}) {
  const requests = []
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const method = request.method()
    if (path === '/api/v1/system/refresh') {
      return route.fulfill({ json: { access_token: 'portal-permissions-token', expires_in: 300 } })
    }
    if (path === '/api/v1/system/users/me') {
      return route.fulfill({ json: { id: 32, username: 'portal-permissions-user' } })
    }
    if (path === '/api/v1/system/auth/context') {
      return route.fulfill({ json: {
        context: { type: 'tenant', tenant_id: '3' },
        authorization: { role_assignments: [{
          scope: { type: 'tenant', tenant_id: '3' }, permissions
        }] }
      } })
    }

    requests.push({ method, path })
    if (path === '/api/v1/portal/assets/42' && method === 'GET') {
      return route.fulfill({ json: { id: 42, name: '测试资产', type_code: 'table', status: 'published' } })
    }
    if (path === '/api/v1/portal/assets/42/apply-status' && method === 'GET' && accessStatus) {
      return route.fulfill({ json: { status: accessStatus } })
    }
    if (path === '/api/v1/portal/assets/42/my-rating' && method === 'GET') {
      return route.fulfill({ json: { rating: ownRating } })
    }
    if (path === '/api/v1/portal/assets/42/ratings' && ['POST', 'PUT'].includes(method)) {
      return route.fulfill({ json: { id: 7, user_id: 32, asset_id: 42, score: 5, comment: '', tags: [] } })
    }
    return route.fulfill({ status: 403, json: { error: 'unexpected_business_request' } })
  })
  await page.goto('/portal/assets/42')
  await expect(page.getByRole('heading', { name: '测试资产' })).toBeVisible()
  return requests
}

test('create-only account can submit without application, authorization or rating read', async ({ page }) => {
  const requests = await openAsset(page, ['asset.entry.read', 'asset.rating.create'])
  await page.getByRole('button', { name: '提交评价' }).click()
  await page.getByRole('dialog').getByRole('button', { name: '提交', exact: true }).click()

  await expect(page.getByRole('button', { name: '提交评价' })).toHaveCount(0)
  expect(requests).toEqual([
    { method: 'GET', path: '/api/v1/portal/assets/42' },
    { method: 'POST', path: '/api/v1/portal/assets/42/ratings' }
  ])
})

test('update-only account loads its own rating and updates without public rating read', async ({ page }) => {
  const requests = await openAsset(page, ['asset.entry.read', 'asset.rating.update'], {
    ownRating: { id: 7, user_id: 32, asset_id: 42, score: 4, comment: '已有评价', tags: [] }
  })
  await page.getByRole('button', { name: '修改评价' }).click()
  await page.getByRole('dialog').getByRole('button', { name: '提交', exact: true }).click()

  await expect(page.getByText('评价已提交')).toBeVisible()
  expect(requests).toEqual([
    { method: 'GET', path: '/api/v1/portal/assets/42' },
    { method: 'GET', path: '/api/v1/portal/assets/42/my-rating' },
    { method: 'PUT', path: '/api/v1/portal/assets/42/ratings' }
  ])
})

test('account without rating permissions makes no rating request or action', async ({ page }) => {
  const requests = await openAsset(page, ['asset.entry.read'])
  await expect(page.getByRole('button', { name: /提交评价|修改评价/ })).toHaveCount(0)
  expect(requests).toEqual([{ method: 'GET', path: '/api/v1/portal/assets/42' }])
})

test('known pending application hides a permitted create action', async ({ page }) => {
  const requests = await openAsset(page, [
    'asset.entry.read', 'asset.rating.create', 'asset.application.read', 'asset.authorization.read'
  ], { accessStatus: 'pending' })
  await expect(page.getByRole('button', { name: '审批中' })).toBeVisible()
  await expect(page.getByRole('button', { name: '提交评价' })).toHaveCount(0)
  expect(requests).toEqual([
    { method: 'GET', path: '/api/v1/portal/assets/42' },
    { method: 'GET', path: '/api/v1/portal/assets/42/apply-status' }
  ])
})
