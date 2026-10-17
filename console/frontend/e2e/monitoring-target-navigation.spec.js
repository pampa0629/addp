import { expect, test } from '@playwright/test'
import { backend, id, identity, node, permissions } from '../../../monitor/frontend/e2e/monitoring-targets.fixture'

test('host management opens manifest admission in Monitor and confirms the exact selected host', async ({ page }) => {
  const state = await backend(page, { empty: true, hosts: [{ node_id: node, display_name: 'Existing VM', node_kind: 'virtual', addresses: [], enabled: true, version: 1, allowed_module_bindings: [] }] })
  await page.goto('/system/host-nodes')
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  await frame.getByRole('button', { name: '从部署清单接入', exact: true }).click()
  await expect(page).toHaveURL(/\/monitor\/monitoring-targets\/new$/)
  const manifest = { schema: 'addp.host-admission/v1', host: { display_name: 'Desktop VM', node_kind: 'virtual', addresses: [], allowed_module_bindings: [] },
    collection: { monitor_kind: 'host_resources', source: { type: 'node_exporter', endpoint: 'https://127.0.0.1:19091/metrics' } } }
  await frame.getByTestId('admission-file').setInputFiles({ name: 'host-admission.json', mimeType: 'application/json', buffer: Buffer.from(JSON.stringify(manifest)) })
  await expect(frame.getByTestId('target-admission')).toContainText('Desktop VM')
  await frame.getByTestId('target-node').click()
  await frame.getByRole('option', { name: 'Existing VM', exact: true }).click()
  await frame.getByTestId('target-save').click()
  await expect(page).toHaveURL(new RegExp(`/monitor/monitoring-targets/${id}$`))
  expect(state.writes).toHaveLength(1)
  expect(state.writes[0].input.subject.node_id).toBe(node)
  await page.reload()
  await expect(frame.getByTestId('target-endpoint')).toHaveValue(manifest.collection.source.endpoint)
  expect(state.writes).toHaveLength(1)
})

test('platform monitoring menu opens the real iframe and preserves a single Console route history', async ({ page }) => {
  await backend(page, { total: 45 })
  await page.goto('/monitor/monitoring-targets?page=2')
  const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(monitor.getByRole('button', { name: '详情', exact: true })).toBeVisible()
  await expect(page.locator('.sidebar .el-menu-item.is-active')).toContainText('采集配置')
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
