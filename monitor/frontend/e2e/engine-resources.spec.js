import { test, expect } from '@playwright/test'
import { engineBackend, engineID } from './engine-resources.fixture'
import { identity, setIdentity } from './monitoring-targets.fixture'
import { enginePermissions, engineOwner } from './engine-resources.fixture'

const hostPermissions = [...enginePermissions, 'platform.host_node.read']

for (const locale of ['zh-cn', 'en']) test(`engine host link preserves detail and trend URL on browser return (${locale})`, async ({ page }, info) => {
  const host = { node_id: engineOwner.node_id, display_name: 'Current managed PostgreSQL host', enabled: true, version: 1 }
  const state = await engineBackend(page, { locale, auth: identity(hostPermissions), hosts: [host] })
  const path = `/engine-resources?search=PostgreSQL&refresh=off&deployment_id=${engineID}&metric=postgresql.database.total_bytes&range=1h`
  await page.goto(path)
  await expect(page.getByTestId('engine-host').getByRole('button', { name: host.display_name, exact: true })).toBeVisible()
  await expect(page.getByTestId('engine-value-capacity')).toHaveText('1.00 GiB')
  await page.screenshot({ path: info.outputPath(`engine-host-${locale}.png`), fullPage: true })
  expect(state.hostReads).toEqual([host.node_id])
  await page.getByTestId('engine-host').getByRole('button').click()
  await expect(page).toHaveURL(new RegExp(`/node-resources/${host.node_id}$`))
  await page.goBack()
  await expect(page).toHaveURL(path)
  await expect(page.getByTestId('engine-host').getByRole('button', { name: host.display_name, exact: true })).toBeVisible()
  await expect(page.getByTestId('engine-trend').getByRole('img', { name: locale === 'en' ? 'Total database size' : '数据库总容量', exact: true })).toBeVisible()
})

test('engine list uses current host name and optional host failure preserves valid metrics', async ({ page }) => {
  const host = { node_id: engineOwner.node_id, display_name: 'Managed host', enabled: true }
  const state = await engineBackend(page, { auth: identity(hostPermissions), hosts: [host] })
  await page.goto('/engine-resources?refresh=off')
  await expect(page.getByTestId('engine-host').getByRole('button')).toHaveText('Managed host')
  host.display_name = 'Renamed host'
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('engine-host').getByRole('button')).toHaveText('Renamed host')
  state.setHostStatus(403)
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('engine-host')).toHaveText(host.node_id)
  await expect(page.getByTestId('engine-host').getByRole('button')).toHaveCount(0)
  await expect(page.getByTestId('engine-list')).toContainText('1.00 GiB')
  await expect(page.getByTestId('engine-list')).not.toContainText('Renamed host')
})

test('engine host permission removal drops a late host response without blocking observation', async ({ page }) => {
  const state = await engineBackend(page, { auth: identity(hostPermissions), hosts: [{ node_id: engineOwner.node_id, display_name: 'Late forbidden host' }], holdHosts: true })
  await page.goto(`/engine-resources?deployment_id=${engineID}&refresh=off`)
  await expect(page.getByTestId('engine-value-capacity')).toHaveText('1.00 GiB')
  await expect.poll(() => state.hostReads.length).toBe(1)
  await setIdentity(page, identity(enginePermissions))
  await expect(page.getByRole('button', { name: '刷新', exact: true })).toBeEnabled()
  state.releaseHosts()
  await expect(page.getByTestId('engine-host')).toHaveText(engineOwner.node_id)
  await expect(page.getByTestId('engine-host').getByRole('button')).toHaveCount(0)
  await expect(page.getByTestId('engine-value-capacity')).toHaveText('1.00 GiB')
  await expect(page.locator('body')).not.toContainText('Late forbidden host')
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByRole('button', { name: '刷新', exact: true })).toBeEnabled()
  expect(state.hostReads).toEqual([engineOwner.node_id])
})

test('an invalid current binding never falls back to the historical declared host', async ({ page }) => {
  const state = await engineBackend(page, { auth: identity(hostPermissions), owner: { ...engineOwner, host_binding_state: 'rejected', node_id: '', declared_node_id: engineOwner.node_id }, hosts: [{ node_id: engineOwner.node_id, display_name: 'Old declared host' }] })
  await page.goto(`/engine-resources?deployment_id=${engineID}&refresh=off`)
  await expect(page.getByTestId('engine-host')).toHaveText('当前无有效主机关联')
  await expect(page.getByTestId('engine-host').getByRole('button')).toHaveCount(0)
  await expect(page.getByTestId('engine-value-capacity')).toHaveText('—')
  expect(state.hostReads).toHaveLength(0)
})

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
