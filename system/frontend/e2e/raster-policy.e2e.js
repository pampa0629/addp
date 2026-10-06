import { expect, test } from '@playwright/test'

import { mockRaster } from './raster-policy.fixture'

test('platform adopts visible resource advice and saves a versioned policy with restart status', async ({ page }) => {
  const writes = await mockRaster(page)
  await page.goto('/configuration')
  const card = page.getByTestId('raster-policy')
  await expect(card).toHaveAttribute('data-state', 'loaded')
  await expect(page.getByTestId('raster-advice-values')).toContainText('缓存 32 MiB')
  await page.getByTestId('raster-advice').click()
  await page.getByTestId('raster-save').click()
  await expect.poll(() => writes.length).toBe(1)
  expect(writes[0]).toEqual({ version: 1, running: 1, waiting: 1, cache_mib: 32, default_tenant_running: 1, default_tenant_waiting: 1 })
  await expect(page.getByTestId('raster-restart')).toBeVisible()
})

test('tenant only writes its quota and can reset to inherited limits', async ({ page }) => {
  const writes = await mockRaster(page, { scope: 'tenant' })
  await page.goto('/configuration')
  await expect(page.getByTestId('raster-policy')).toHaveAttribute('data-state', 'loaded')
  await expect(page.getByTestId('cache_mib')).toHaveCount(0)
  await expect(page.getByTestId('raster-advice')).toHaveCount(0)
  await expect(page.getByTestId('raster-constraints')).toContainText('256')
  await page.getByTestId('inherit-running').click()
  await page.getByTestId('running').getByRole('spinbutton').fill('1')
  await page.getByTestId('raster-save').click()
  await expect.poll(() => writes.length).toBe(1)
  expect(writes[0]).toEqual({ version: 1, running: 1, waiting: null })
  await page.getByTestId('inherit-running').click()
  await page.getByTestId('raster-save').click()
  await expect.poll(() => writes.length).toBe(2)
  expect(writes[1]).toEqual({ version: 2, running: null, waiting: null })
})

test('read-only role has no save action and stale writes keep the administrator draft', async ({ page }) => {
  await mockRaster(page, { writable: false })
  await page.goto('/configuration')
  await expect(page.getByTestId('raster-policy')).toHaveAttribute('data-state', 'loaded')
  await expect(page.getByTestId('raster-save')).toHaveCount(0)
  await expect(page.getByTestId('running').getByRole('spinbutton')).toBeDisabled()
})

test('version conflict keeps the draft', async ({ page }) => {
  await mockRaster(page, { conflict: true })
  await page.goto('/configuration')
  await expect(page.getByTestId('raster-policy')).toHaveAttribute('data-state', 'loaded')
  await page.getByTestId('cache_mib').getByRole('spinbutton').fill('128')
  await page.getByTestId('raster-save').click()
  await expect(page.getByText(/配置已被修改，请刷新后重新编辑/)).toBeVisible()
  await expect(page.getByTestId('cache_mib').getByRole('spinbutton')).toHaveValue('128')
})

for (const empty of [false, true]) test(`unavailable policy or empty engine list exposes no fabricated defaults (${empty})`, async ({ page }) => {
  await mockRaster(page, { unavailable: true, empty })
  await page.goto('/configuration')
  await expect(page.getByTestId('raster-policy')).toHaveAttribute('data-state', 'unavailable')
  await expect(page.getByTestId('raster-save')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '刷新', exact: true })).not.toHaveClass(/is-loading/)
})


test('platform shrink displays the saved tenant quota without silently overwriting it', async ({ page }) => {
  await mockRaster(page, { scope: 'tenant', shrunk: true })
  await page.goto('/configuration')
  await expect(page.getByTestId('raster-policy')).toHaveAttribute('data-state', 'loaded')
  await expect(page.getByTestId('running').getByRole('spinbutton')).toHaveValue('2')
  await expect(page.getByTestId('raster-quota-exceeds')).toBeVisible()
  await page.getByTestId('running').getByRole('spinbutton').fill('1')
  await expect(page.getByTestId('running').getByRole('spinbutton')).toHaveValue('1')
})


test('configuration groups are permission filtered and the old raster route is removed', async ({ page }) => {
  await mockRaster(page, { scope: 'tenant' })
  await page.goto('/configuration?tab=security-policy')
  await expect(page.getByRole('tab', { name: '栅格引擎资源策略' })).toBeVisible()
  await expect(page.getByRole('tab', { name: 'IAM 安全策略', exact: true })).toHaveCount(0)
  await expect(page.getByTestId('raster-policy')).toHaveAttribute('data-state', 'loaded')
  await page.goto('/engine-raster-policies')
  await expect(page.getByTestId('raster-policy')).toHaveCount(0)
})

test('tenant configuration fits a narrow viewport and retains the existing English labels', async ({ page }) => {
  await page.setViewportSize({ width: 480, height: 900 })
  await mockRaster(page, { scope: 'tenant', language: 'en' })
  await page.goto('/configuration')
  await expect(page.getByTestId('raster-policy')).toHaveAttribute('data-state', 'loaded')
  await expect(page.getByRole('heading', { name: 'Configuration Management', exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

for (const rasterAccess of [false, true]) test(`platform configuration preserves independent domain permissions (${rasterAccess})`, async ({ page }, testInfo) => {
  await mockRaster(page, { writable: false, permissions: ['iam.security_policy.read', ...(rasterAccess ? ['system.engine_raster_policy.read'] : [])] })
  await page.route('**/api/v1/system/platform/security_policy', route => route.fulfill({ json: {
    version: 1, applied_version: 1, pending_restart: false,
    access_token_ttl_minutes: 15, delegated_access_token_ttl_minutes: 2,
    resource_access_ticket_ttl_minutes: 15, refresh_token_ttl_days: 30,
    oauth_authorization_code_ttl_minutes: 5, oauth_device_code_ttl_minutes: 10,
    oauth_device_poll_interval_seconds: 5, tenant_invitation_ttl_hours: 168,
    oauth_public_rate_limit_per_minute: 60, oauth_user_rate_limit_per_minute: 30
  } }))
  await page.goto('/configuration')
  await expect(page.getByRole('tab', { name: 'IAM 安全策略', exact: true })).toBeVisible()
  await expect(page.locator('.security-policy')).toContainText('会话与访问凭据')
  await expect(page.locator('.security-policy .form-actions')).toHaveCount(0)
  const rasterTab = page.getByRole('tab', { name: '栅格引擎资源策略', exact: true })
  if (!rasterAccess) {
    await expect(rasterTab).toHaveCount(0)
    await expect(page.getByTestId('raster-policy')).toHaveCount(0)
    return
  }
  await rasterTab.click()
  await expect(page).toHaveURL(/tab=raster-policy/)
  await expect(page.getByTestId('raster-policy')).toHaveAttribute('data-state', 'loaded')
  await expect(page.getByTestId('raster-save')).toHaveCount(0)
  await page.reload()
  await expect(rasterTab).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByTestId('raster-policy')).toHaveAttribute('data-state', 'loaded')
  await testInfo.attach('system-configuration', { body: await page.screenshot(), contentType: 'image/png' })
})
