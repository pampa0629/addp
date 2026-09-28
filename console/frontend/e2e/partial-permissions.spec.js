import { expect, test } from '@playwright/test'

async function openAsTenant(page, route, assignments = [], tenantID = '3') {
  const businessRequests = []
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.context().route('**/api/v1/system/**', async requestRoute => {
    const path = new URL(requestRoute.request().url()).pathname
    if (path.endsWith('/refresh')) {
      return requestRoute.fulfill({ json: { access_token: 'limited-tenant-token', expires_in: 300 } })
    }
    if (path.endsWith('/users/me')) {
      return requestRoute.fulfill({ json: { id: 17, username: 'limited-user' } })
    }
    if (path.endsWith('/auth/context')) {
      return requestRoute.fulfill({ json: {
        context: { type: 'tenant', tenant_id: tenantID },
        authorization: { role_assignments: assignments }
      } })
    }
    return requestRoute.fulfill({ status: 404, json: { error: 'unexpected_system_request' } })
  })
  await page.context().route('**/api/v1/transfer/**', async requestRoute => {
    businessRequests.push(requestRoute.request().url())
    return requestRoute.fulfill({ status: 403, json: { error: 'forbidden' } })
  })
  await page.route('http://127.0.0.1:5176/**', requestRoute =>
    requestRoute.fulfill({ contentType: 'text/html', body: '<title>Transfer fixture</title>' }))
  await page.goto(route)
  return businessRequests
}

function assignment(permissions, scope = { type: 'tenant', tenant_id: '3' }) {
  return { scope, permissions }
}

test('an account without Transfer permission sees no business iframe or request on a direct URL', async ({ page }) => {
  const businessRequests = await openAsTenant(page, '/transfer/tasks', [])
  await expect(page.locator('.el-result')).toBeVisible()
  await expect(page.locator('iframe.module-iframe')).toHaveCount(0)
  expect(businessRequests).toEqual([])
})

test('a create-only account enters the wizard but cannot enter the task list', async ({ page }) => {
  const businessRequests = await openAsTenant(page, '/transfer/tasks/create', [
    assignment(['transfer.task.create', 'meta.catalog.read'])
  ])
  await expect(page.locator('iframe.module-iframe')).toHaveAttribute('src', /\/tasks\/create$/)
  await expect(page.locator('.sidebar .el-menu-item.is-active')).toContainText('创建传输任务')
  await page.goto('/transfer/tasks')
  await expect(page.locator('.el-result')).toBeVisible()
  await expect(page.locator('iframe.module-iframe')).toHaveCount(0)
  expect(businessRequests).toEqual([])
})

test('an account that can read and create uses the task list as its single sidebar entry', async ({ page }) => {
  await openAsTenant(page, '/transfer/tasks', [
    assignment(['transfer.task.read', 'transfer.task.create', 'meta.catalog.read'])
  ])
  await expect(page.locator('.sidebar .el-menu-item')).toHaveCount(1)
  await expect(page.locator('.sidebar .el-menu-item.is-active')).toContainText('传输任务')
  await page.goto('/transfer/tasks/create')
  await expect(page.locator('iframe.module-iframe')).toHaveAttribute('src', /\/tasks\/create$/)
  await expect(page.locator('.sidebar .el-menu-item.is-active')).toContainText('传输任务')
})

test('a read-only account enters the task list but cannot enter the wizard', async ({ page }) => {
  const businessRequests = await openAsTenant(page, '/transfer/tasks', [
    assignment(['transfer.task.read'])
  ])
  await expect(page.locator('iframe.module-iframe')).toHaveAttribute('src', /\/tasks$/)
  await page.goto('/transfer/tasks/create')
  await expect(page.locator('.el-result')).toBeVisible()
  await expect(page.locator('iframe.module-iframe')).toHaveCount(0)
  expect(businessRequests).toEqual([])
})

for (const [name, scope] of [
  ['another tenant', { type: 'tenant', tenant_id: '4' }],
  ['department', { type: 'department', tenant_id: '3', department_id: '9' }],
  ['project group', { type: 'project_group', tenant_id: '3', project_group_id: '11' }]
]) {
  test(`${name} scope does not unlock the tenant-wide task list`, async ({ page }) => {
    const businessRequests = await openAsTenant(page, '/transfer/tasks', [
      assignment(['transfer.task.read'], scope)
    ])
    await expect(page.locator('.el-result')).toBeVisible()
    await expect(page.locator('iframe.module-iframe')).toHaveCount(0)
    expect(businessRequests).toEqual([])
  })
}

test('an account without Portal pages sees no Portal entry on the Console home', async ({ page }) => {
  await openAsTenant(page, '/', [])
  await expect(page.getByText('当前账号暂无可用业务模块')).toBeVisible()
  await expect(page.getByText('请从顶部选择功能区域，或直接点击模块卡片进入')).toHaveCount(0)
  await expect(page.getByText('所有模块', { exact: true })).toHaveCount(0)
  await expect(page.getByRole('heading', { name: '数据门户' })).toHaveCount(0)
  await expect(page.locator('.copilot-fab-wrapper')).toHaveCount(0)
})

test('an applications-only account opens its accessible Portal page', async ({ page }) => {
  await openAsTenant(page, '/', [assignment(['asset.application.read'])])
  await page.getByText('所有模块', { exact: true }).click()
  const portalCard = page.locator('.module-card').filter({ has: page.getByRole('heading', { name: '数据门户' }) })
  await expect(portalCard).toBeVisible()
  const popupPromise = page.waitForEvent('popup')
  await portalCard.click()
  const popup = await popupPromise
  await expect(popup).toHaveURL(/\/portal\/my\/applications$/)
  await popup.close()
})
