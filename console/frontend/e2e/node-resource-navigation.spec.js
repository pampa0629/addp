import { expect, test } from '@playwright/test'
import { identity, node, resourceBackend, resourcePermissions } from '../../../monitor/frontend/e2e/node-resources.fixture'

test('node resource navigation preserves the real Monitor iframe and one Console history', async ({ page }) => {
  await resourceBackend(page)
  await page.goto('/monitor/node-resources?page=2')
  const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(monitor.getByRole('button', { name: '查看资源' })).toHaveCount(2)
  await expect(page.locator('.sidebar .el-menu-item.is-active')).toContainText('主机监控')
  const frame = page.frames().find(frame => frame.parentFrame())
  const documentID = await frame.evaluate(() => performance.timeOrigin)
  await monitor.getByRole('button', { name: '查看资源' }).first().click()
  await expect(page).toHaveURL(new RegExp(`/monitor/node-resources/${node}\\?page=2$`))
  await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  expect(await frame.evaluate(() => performance.timeOrigin)).toBe(documentID)
  await page.goBack()
  await expect(monitor.getByRole('button', { name: '查看资源' })).toHaveCount(2)
  await page.goForward()
  await expect(monitor.getByTestId('resource-cores')).toContainText('0 核')
  await page.reload()
  await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
})
test('Tenant navigation hides the entry and never mounts the platform resource iframe', async ({ page }) => {
  const state = await resourceBackend(page, { identity: identity(resourcePermissions, { type: 'tenant', tenant_id: '7' }) })
  await page.goto('/monitor/node-resources')
  await expect(page.locator('.el-result')).toBeVisible()
  await expect(page.locator('iframe.module-iframe')).toHaveCount(0)
  expect(state.reads).toHaveLength(0)
})
