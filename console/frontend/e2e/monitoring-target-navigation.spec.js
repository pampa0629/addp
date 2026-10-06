import { expect, test } from '@playwright/test'
import { backend, id, identity, permissions } from '../../../monitor/frontend/e2e/monitoring-targets.fixture'

test('platform monitoring menu opens the real iframe and preserves a single Console route history', async ({ page }) => {
  await backend(page, { total: 45 })
  await page.goto('/monitor/monitoring-targets?page=2')
  const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(monitor.getByRole('button', { name: '详情', exact: true })).toBeVisible()
  await expect(page.locator('.sidebar .el-menu-item.is-active')).toContainText('平台监测目标')
  const frame = page.frames().find(frame => frame.parentFrame())
  const documentID = await frame.evaluate(() => performance.timeOrigin)
  await monitor.getByRole('button', { name: '详情', exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/monitor/monitoring-targets/${id}\\?page=2$`))
  await expect(monitor.getByTestId('target-endpoint')).toHaveValue('https://node.test:9443/metrics')
  expect(await frame.evaluate(() => performance.timeOrigin)).toBe(documentID)
  await page.reload()
  await expect(monitor.getByTestId('target-endpoint')).toHaveValue('https://node.test:9443/metrics')
  await monitor.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(page).toHaveURL(/\/monitor\/monitoring-targets\?page=2$/)
})
test('a tenant cannot load the monitoring iframe even with platform permission strings', async ({ page }) => {
  const state = await backend(page, { identity: identity(permissions, { type: 'tenant', tenant_id: '7' }) })
  await page.goto('/monitor/monitoring-targets')
  await expect(page.locator('.el-result')).toBeVisible()
  await expect(page.locator('iframe.module-iframe')).toHaveCount(0)
  expect(state.reads).toHaveLength(0)
})
