import { expect, test } from '@playwright/test'
import { identity } from '../../../monitor/frontend/e2e/monitoring-targets.fixture'
import { overviewBackend, overviewNodeID, overviewPermissions } from '../../../monitor/frontend/e2e/resource-overview.fixture'

 test('resource overview keeps one Console history through host navigation and refresh restore', async ({ page }) => {
  await overviewBackend(page)
  const startupFailuresPath = '/api/v1/system/platform/module_startup_failures'
  await page.route(`**${startupFailuresPath}`, route => {
    expect(route.request().method()).toBe('GET')
    return route.fulfill({ json: { data: [] } })
  })
  await page.goto('/monitor/resource-overview?refresh=off')
  const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(monitor.getByTestId('overview-rank-0')).toContainText('完整范围主机41')
  await expect(page.locator('.sidebar .el-menu-item.is-active')).toContainText('资源总览')
  const frame = page.frames().find(frame => frame.parentFrame()), timeOrigin = await frame.evaluate(() => performance.timeOrigin)
  await monitor.getByTestId('overview-rank-0').getByRole('button', { name: '完整范围主机41', exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/monitor/node-resources/${overviewNodeID}$`))
  await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  await expect(monitor.getByTestId('resource-overview')).toHaveCount(0)
  expect(await frame.evaluate(() => performance.timeOrigin)).toBe(timeOrigin)
  await page.goBack(); await expect(monitor.getByTestId('overview-rank-0')).toBeVisible()
  await expect(page).toHaveURL(/resource-overview\?refresh=off/)
  await page.reload(); await expect(monitor.getByTestId('overview-refresh-status')).toContainText('手工刷新')
  const [startupFailures] = await Promise.all([
    page.waitForResponse(response => new URL(response.url()).pathname === startupFailuresPath),
    monitor.getByRole('button', { name: '服务监控', exact: true }).click()
  ])
  await expect(page).toHaveURL(/\/monitor\/service-resources$/)
  await expect(monitor.getByTestId('resource-overview')).toHaveCount(0)
  expect(startupFailures.status()).toBe(200)
  await expect(monitor.getByTestId('process-list')).toBeVisible()
  await expect(monitor.getByTestId('process-startup-failures')).toHaveCount(0)
 })
 test('resource overview does not mount in Tenant context even with platform permission strings', async ({ page }) => {
  const state = await overviewBackend(page, { identity: identity(overviewPermissions, { type: 'tenant', tenant_id: '7' }) })
  await page.goto('/monitor/resource-overview'); await expect(page.locator('.el-result')).toBeVisible()
  await expect(page.locator('iframe.module-iframe')).toHaveCount(0); expect(state.calls).toHaveLength(0)
 })
 test('resource overview hides service navigation without the separate module permission', async ({ page }) => {
  await overviewBackend(page, { identity: identity(['monitor.resource_observation.read', 'platform.host_node.read']) })
  await page.goto('/monitor/resource-overview?refresh=off')
  const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(monitor.getByTestId('overview-rank-0')).toBeVisible()
  await expect(monitor.getByRole('button', { name: '服务监控', exact: true })).toHaveCount(0)
 })
