import { expect, test } from '@playwright/test'

const read = 'manager.configuration.read'
const update = 'manager.configuration.update'
const quickViewPath = '/api/v1/manager/settings/quick-view-policy'
const baseMapPath = '/api/v1/manager/settings/base-map/providers'
const embeddingPermissions = ['inference.profile.read', 'inference.deployment.read']
const policy = {
  version: 7,
  direct_flatgeobuf_max_rows: 3500,
  realtime_tile_timeout_ms: 3100,
  realtime_tile_retry_after_sec: 45,
  raster_mosaic_generation_timeout_seconds: 8100
}

async function openConfiguration(page, scope, permissions, { failRead = false, startPath = '/configuration/manager' } = {}) {
  const requests = []
  const writes = []
  const context = scope === 'tenant' ? { type: scope, tenant_id: '3' } : { type: scope }
  let saved = { ...policy }
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', route => {
    const pathname = new URL(route.request().url()).pathname
    const method = route.request().method()
    switch (pathname) {
      case '/api/v1/system/refresh':
        return route.fulfill({ json: { access_token: 'manager-configuration-token', expires_in: 300 } })
      case '/api/v1/system/users/me':
        return route.fulfill({ json: { id: 32, username: 'configuration-user' } })
      case '/api/v1/system/auth/context':
        return route.fulfill({ json: {
          context,
          authorization: { role_assignments: [{ scope: context, permissions }] }
        } })
      case '/api/v1/system/configuration-management/entries':
        return route.fulfill({ json: [{
          id: 'manager.configuration',
          owner_module: 'manager',
          scope_types: ['platform_default_with_tenant_override'],
          frontend_route: '/configuration/manager',
          available: true
        }] })
      case baseMapPath:
        requests.push([method, pathname])
        if (method === 'GET' && permissions.includes(read)) return route.fulfill({ json: { data: [] } })
        break
      case quickViewPath:
        requests.push([method, pathname])
        if (scope !== 'platform' || !permissions.includes(read)) break
        if (method === 'GET') {
          return failRead
            ? route.fulfill({ status: 503, json: { error: '快显策略暂时不可用' } })
            : route.fulfill({ json: saved })
        }
        if (method === 'PUT' && permissions.includes(update)) {
          const input = route.request().postDataJSON()
          writes.push(input)
          saved = { ...input, version: saved.version + 1 }
          return route.fulfill({ json: saved })
        }
        break
    }
    return route.fulfill({ status: 403, json: { error: 'unexpected_business_request' } })
  })
  await page.goto(startPath)
  return { requests, writes }
}

test('tenant with configuration and model read permissions only requests the base map settings', async ({ page }) => {
  const { requests } = await openConfiguration(page, 'tenant', [read, update, ...embeddingPermissions])
  await expect(page.getByRole('tab', { name: '底图服务', exact: true })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('row').filter({ hasText: 'osm' })).toBeVisible()
  await expect(page.getByRole('tab', { name: '快显策略', exact: true })).toHaveCount(0)
  await expect(page.getByRole('tab', { name: /^(向量化|影像分割)$/ })).toHaveCount(0)
  expect(requests).toEqual([['GET', baseMapPath]])
})

for (const scope of ['tenant', 'platform']) {
  test(`${scope} without Manager read permission cannot open configuration or request its values`, async ({ page }) => {
    const { requests } = await openConfiguration(page, scope, [update])
    await expect(page.locator('.el-result')).toContainText('无权访问')
    await expect(page.getByTestId('configuration-management')).toHaveCount(0)
    expect(requests).toEqual([])
  })
}

test('platform reader sees the saved policy with editing disabled', async ({ page }) => {
  const { requests } = await openConfiguration(page, 'platform', [read])
  await expect(page.getByRole('tab', { name: '快显策略', exact: true })).toHaveAttribute('aria-selected', 'true')
  const fields = page.getByRole('spinbutton')
  await expect(fields).toHaveCount(4)
  await expect(fields.first()).toHaveValue('3500')
  for (const field of await fields.all()) await expect(field).toBeDisabled()
  await expect(page.getByRole('button', { name: '保存', exact: true })).toBeDisabled()
  await expect(page.getByRole('tab', { name: /^(向量化|影像分割)$/ })).toHaveCount(0)
  expect(requests).toEqual([['GET', quickViewPath]])
})

async function stubManagerInferencePage(page, documentReady) {
  await page.route(url => url.pathname === '/module-ui/manager/settings/embedding', async route => {
    await documentReady
    const segmentation = new URL(route.request().url()).searchParams.get('tab') === 'segmentation'
    await route.fulfill({
      contentType: 'text/html',
      body: segmentation ? '<h1>Manager segmentation page</h1>' : '<h1>Manager embedding page</h1>'
    })
  })
}

test('platform reader stays in Manager configuration when switching between its two inference tabs', async ({ page }) => {
  const { promise: documentReady, resolve: releaseDocument } = Promise.withResolvers()
  await stubManagerInferencePage(page, documentReady)
  await openConfiguration(page, 'platform', [read, ...embeddingPermissions], { startPath: '/configuration' })
  await page.getByRole('row', { name: /Manager 配置/ }).click()
  const configuration = page.getByTestId('configuration-management')
  const managerHeading = configuration.getByRole('heading', { level: 1, name: '数据管理' })
  const embeddingTab = configuration.getByRole('tab', { name: '向量化', exact: true })
  const segmentationTab = configuration.getByRole('tab', { name: '影像分割', exact: true })
  const quickViewTab = configuration.getByRole('tab', { name: '快显策略', exact: true })
  const iframe = configuration.frameLocator('iframe.module-iframe')
  await expect(managerHeading).toBeVisible()
  await expect(embeddingTab).toBeVisible()
  try {
    await embeddingTab.click()
    await expect(configuration.locator('iframe.module-iframe')).toHaveCSS('visibility', 'hidden')
  } finally {
    releaseDocument()
  }
  await expect(page).toHaveURL(/\/manager\/settings\/embedding$/)
  await expect(managerHeading).toBeVisible()
  await expect(page.getByRole('menuitem', { name: '配置管理', exact: true })).toHaveClass(/\bis-active\b/)
  await expect(embeddingTab).toHaveAttribute('aria-selected', 'true')
  await expect(iframe.getByRole('heading', { name: 'Manager embedding page' })).toBeVisible()
  const tabBounds = await embeddingTab.boundingBox()
  const frameBounds = await configuration.locator('iframe.module-iframe').boundingBox()
  expect(tabBounds.y + tabBounds.height).toBeLessThanOrEqual(frameBounds.y)
  await segmentationTab.click()
  await expect(page).toHaveURL(/\/manager\/settings\/embedding\?tab=segmentation$/)
  await expect(segmentationTab).toHaveAttribute('aria-selected', 'true')
  await expect(iframe.getByRole('heading', { name: 'Manager segmentation page' })).toBeVisible()
  await page.goBack()
  await expect(embeddingTab).toHaveAttribute('aria-selected', 'true')
  await expect(iframe.getByRole('heading', { name: 'Manager embedding page' })).toBeVisible()
  await quickViewTab.click()
  await expect(page).toHaveURL(/\/configuration\/manager$/)
  await expect(quickViewTab).toHaveAttribute('aria-selected', 'true')
  await expect(configuration.locator('iframe')).toHaveCount(0)
  await page.goBack()
  await expect(embeddingTab).toHaveAttribute('aria-selected', 'true')
})

test('platform reader opening the segmentation URL directly can return to the configuration list', async ({ page }) => {
  await stubManagerInferencePage(page)
  await openConfiguration(page, 'platform', [read, ...embeddingPermissions], { startPath: '/manager/settings/embedding?tab=segmentation' })
  const configuration = page.getByTestId('configuration-management')
  await expect(configuration.getByRole('heading', { level: 1, name: '数据管理' })).toBeVisible()
  await expect(configuration.getByRole('tab', { name: '影像分割', exact: true })).toHaveAttribute('aria-selected', 'true')
  await expect(configuration.frameLocator('iframe.module-iframe').getByRole('heading', { name: 'Manager segmentation page' })).toBeVisible()
  await configuration.getByRole('button', { name: '返回配置入口' }).click()
  await expect(page).toHaveURL(/\/configuration$/)
  await expect(configuration.getByRole('row', { name: /Manager 配置/ })).toBeVisible()
})

test('platform editor saves the loaded version and the four quick-view settings', async ({ page }) => {
  const { requests, writes } = await openConfiguration(page, 'platform', [read, update])
  const rows = page.getByRole('spinbutton').first()
  await expect(rows).toHaveValue('3500')
  await rows.fill('4000')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect.poll(() => requests).toEqual([['GET', quickViewPath], ['PUT', quickViewPath], ['GET', quickViewPath]])
  expect(writes).toEqual([{ ...policy, direct_flatgeobuf_max_rows: 4000 }])
  await expect(rows).toHaveValue('4000')
})

test('platform editor cannot save or see defaults when the initial read fails', async ({ page }) => {
  const { requests } = await openConfiguration(page, 'platform', [read, update], { failRead: true })
  await expect(page.getByText('快显策略暂时不可用', { exact: true })).toBeVisible()
  await expect(page.getByRole('spinbutton')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '保存', exact: true })).toHaveCount(0)
  expect(requests).toEqual([['GET', quickViewPath]])
})
