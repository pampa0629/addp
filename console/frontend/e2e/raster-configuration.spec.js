import { expect, test } from '@playwright/test'
import { mockRaster } from '../../../system/frontend/e2e/raster-policy.fixture'

test('configuration management opens the single System raster group with the tenant permissions', async ({ page }) => {
  await mockRaster(page, { scope: 'tenant' })
  await page.goto('/configuration')
  const configuration = page.getByTestId('configuration-management')
  await expect(configuration).toHaveAttribute('data-load-state', 'loaded')
  await expect(configuration.getByRole('row').filter({ hasText: 'System 配置' })).toHaveCount(1)
  await expect(page.locator('.portal-sidebar').getByText('栅格引擎资源策略', { exact: true })).toHaveCount(0)
  await configuration.getByRole('row').filter({ hasText: 'System 配置' }).click()
  await expect(page).toHaveURL(/\/system\/configuration$/)
  const system = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(system.getByTestId('raster-policy')).toHaveAttribute('data-state', 'loaded')
  await expect(system.getByRole('tab', { name: 'IAM 安全策略', exact: true })).toHaveCount(0)
  await expect(page.locator('.el-menu-item.is-active').filter({ hasText: '配置管理' })).toBeVisible()
  await page.reload()
  await expect(system.getByTestId('raster-policy')).toHaveAttribute('data-state', 'loaded')
})
