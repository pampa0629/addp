import { test, expect } from '@playwright/test'
import { engineBackend, engineID } from './engine-resources.fixture'
import { identity } from './monitoring-targets.fixture'
import { enginePermissions } from './engine-resources.fixture'

for (const locale of ['zh-cn', 'en']) test(`PostgreSQL direct detail shows units, zeros and current-container trend (${locale})`, async ({ page }, info) => {
  await engineBackend(page, { locale })
  await page.goto(`/engine-resources?deployment_id=${engineID}&range=7d&refresh=off`)
  await expect(page.getByTestId('engine-value-total')).toHaveText(locale === 'en' ? '0 connections' : '0 个')
  await expect(page.getByTestId('engine-value-capacity')).toHaveText('1.00 GiB')
  await expect(page.getByTestId('engine-value-committed')).toHaveText(locale === 'en' ? '0.00 transactions/s' : '0.00 次/秒')
  await expect(page.getByTestId('engine-trend').getByRole('img', { name: locale === 'en' ? 'Commits per second' : '每秒事务提交', exact: true })).toBeVisible()
  await expect(page.getByTestId('engine-trend')).toContainText(locale === 'en' ? 'Current database container' : '当前数据库容器')
  await expect(page.getByTestId('engine-metrics')).not.toContainText('/metrics')
  await page.screenshot({ path: info.outputPath(`postgresql-${locale}.png`), fullPage: true })
})
test('admitted deployment list opens monitoring without manual registration', async ({ page }) => {
  await engineBackend(page)
  await page.goto('/engine-resources?refresh=off')
  await expect(page.getByTestId('engine-list')).toContainText('Infra PostgreSQL')
  await page.getByRole('button', { name: '查看监控', exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`deployment_id=${engineID}`))
  await expect(page.getByTestId('engine-value-active')).toHaveText('0 个')
  await page.goBack()
  await expect(page).toHaveURL(/engine-resources\?refresh=off$/)
  await page.goForward()
  await expect(page.getByTestId('engine-value-active')).toHaveText('0 个')
  await page.getByRole('button', { name: '返回引擎列表', exact: true }).click()
  await expect(page).toHaveURL(/engine-resources\?refresh=off$/)
})
for (const mode of ['unavailable', 'denied', 'notFound', 'foreign', 'sql-failed']) test(`PostgreSQL ${mode} clears old values`, async ({ page }) => {
  const state = await engineBackend(page)
  await page.goto(`/engine-resources?deployment_id=${engineID}&refresh=off`)
  await expect(page.getByTestId('engine-value-capacity')).toHaveText('1.00 GiB')
  state.setMode(mode)
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('engine-value-capacity')).toHaveText('—')
  if (mode === 'sql-failed') await expect(page.getByText('未取得有效数据库快照', { exact: true })).toBeVisible()
  else await expect(page.getByTestId('engine-error')).toBeVisible()
})
test('engine monitoring rejects tenant, machine, delegated and missing owner permission before reading deployments', async ({ page }) => {
  for (const auth of [identity(enginePermissions, { type: 'tenant', tenant_id: '7' }), identity(enginePermissions, { type: 'platform' }, 'service_principal'), identity(enginePermissions, { type: 'platform' }, 'user', {}), identity(['monitor.resource_observation.read']), identity(['platform.engine_deployment.read'])]) {
    const state = await engineBackend(page, { auth })
    await page.goto('/engine-resources?refresh=off')
    await expect(page.getByTestId('engine-denied').or(page.locator('.el-result'))).toBeVisible()
    expect(state.calls).toHaveLength(0)
    await page.unrouteAll({ behavior: 'wait' })
  }
})
