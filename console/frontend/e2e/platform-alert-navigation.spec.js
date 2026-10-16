import { expect, test } from '@playwright/test'
import { mockModuleQueryAPI } from '../../../system/frontend/e2e/module-query.fixture'
import { backend, identity } from '../../../monitor/frontend/e2e/monitoring-targets.fixture'

test('platform alert filters and notifications preserve one Console iframe and restore on reload', async ({ page }) => {
  await backend(page, { identity: identity(['monitor.platform_alert.read', 'monitor.platform_alert_notification.read']) })
  const reads = []
  await page.route('**/api/v1/monitor/platform/**', route => {
    const url = new URL(route.request().url()); reads.push(url.pathname)
    return route.fulfill({ json: url.pathname.endsWith('/destinations') ? [] : url.pathname.endsWith('/alert-notification-destinations') ? [] : { data: [], total: 0, page: 1, page_size: 20 } })
  })
  await page.goto('/monitor/platform-alerts?category=resource')
  const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(monitor.getByRole('heading', { name: '平台告警', exact: true })).toBeVisible()
  await expect(page.locator('.sidebar .el-menu-item.is-active')).toContainText('平台告警')
  const frame = page.frames().find(frame => frame.parentFrame())
  const documentID = await frame.evaluate(() => performance.timeOrigin)
  await monitor.getByRole('button', { name: '通知管理', exact: true }).click()
  await expect(page).toHaveURL(/\/monitor\/platform-alerts\?tab=notifications&category=resource$/)
  expect(await frame.evaluate(() => performance.timeOrigin)).toBe(documentID)
  await page.reload()
  await expect(monitor.getByTestId('platform-notifications')).toBeVisible()
  expect(reads.some(path => path.includes('/log-'))).toBe(false)
})
test('Tenant cannot open platform alerts using a platform permission string', async ({ page }) => {
  await backend(page, { identity: identity(['monitor.platform_alert.read'], { type: 'tenant', tenant_id: '7' }) })
  await page.goto('/monitor/platform-alerts')
  await expect(page.locator('.el-result')).toBeVisible()
  await expect(page.locator('iframe.module-iframe')).toHaveCount(0)
})

test('System diagnostics navigate to the sole Monitor management page with observer context', async ({ page }) => {
  await mockModuleQueryAPI(page, { pipelinePermission: true })
  const requests = []
  await page.route('**/api/v1/monitor/platform/**', route => {
    const path = new URL(route.request().url()).pathname; requests.push(path)
    return route.fulfill({ json: path.endsWith('/log-pipeline') ? { deployment_state: 'enabled', configured: true, health: 'alert', active_incidents: 1, notifications: 'unconfigured', node: { node: 'host-a', received_at: new Date().toISOString(), registry_valid: true, observation: { probe_delivered: true, probe_delay_ms: 10 } } } : { data: [], total: 0, page: 1, page_size: 20 } })
  })
  await page.goto('/system/modules?tab=log-pipeline')
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(frame.getByTestId('pipeline-health')).toContainText('告警')
  await frame.getByRole('button', { name: '查看平台告警', exact: true }).click()
  await expect(page).toHaveURL(/\/monitor\/platform-alerts\?category=log_pipeline&subject_id=host-a$/)
  await expect(frame.getByRole('heading', { name: '平台告警', exact: true })).toBeVisible()
  await expect(frame.getByPlaceholder('来源主体 ID')).toHaveValue('host-a')
  await expect(page.locator('iframe[data-testid="module-iframe"]')).toHaveCount(1)
  expect(requests.some(path => path.endsWith('/alerts'))).toBe(true)
  await page.goBack()
  await expect(page).toHaveURL(/\/system\/modules\?tab=log-pipeline$/)
  await expect(frame.getByTestId('pipeline-health')).toContainText('告警')
})
