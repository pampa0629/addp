import { expect, test } from '@playwright/test'
import { readFile } from 'node:fs/promises'
import { backend, id, identity, node, permissions } from '../../../monitor/frontend/e2e/monitoring-targets.fixture'

for (const [path, title] of [['host-nodes', '平台主机管理'], ['engine-deployments', '引擎纳管']]) {
  test(`first dark-mode render uses the primary text color on ${path}`, async ({ page }) => {
    await backend(page, { identity: identity([...permissions, 'platform.engine_deployment.read']) })
    await page.addInitScript(() => localStorage.setItem('theme-mode', 'dark'))
    await page.route('**/api/v1/system/platform/engine_deployments?**', route => route.fulfill({ json: { data: [], total: 0 } }))
    await page.goto(`/system/${path}`)
    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(frame.locator('html')).toHaveClass(/\bdark\b/)
    await expect(frame.getByRole('heading', { name: title, exact: true })).toHaveCSS('color', 'rgb(229, 234, 243)')
  })
}

test('host management admits the exact existing host in System and hands a source to Monitor only in memory', async ({ page }) => {
  const state = await backend(page, {
    empty: true,
    identity: identity([...permissions, 'platform.module.read', 'platform.engine_deployment.read']),
    hosts: [{ node_id: node, display_name: 'Existing VM', node_kind: 'virtual', addresses: [], enabled: true, version: 1, allowed_module_bindings: [{ module_name: 'meta', client_id: 'addp-meta' }] }]
  })
  await page.goto('/system/host-nodes')
  const deploymentGroup = page.locator('.sidebar .el-sub-menu__title').filter({ hasText: /^部署与纳管$/ }).locator('..')
  await expect(deploymentGroup.locator('.el-menu-item')).toHaveText(['主机管理', '模块管理', '引擎纳管'])
  await expect(deploymentGroup.locator('.el-menu-item').first()).toBeVisible()
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  await frame.getByRole('button', { name: '从部署清单接入', exact: true }).click()
  await expect(page).toHaveURL(/\/system\/host-admission$/)
  const [download] = await Promise.all([
    page.waitForEvent('download'),
    frame.getByRole('link', { name: '下载清单样例', exact: true }).click()
  ])
  expect(download.suggestedFilename()).toBe('addp-host-admission.example.json')
  const file = await download.path()
  const manifest = JSON.parse(await readFile(file, 'utf8'))
  await frame.getByTestId('admission-file').setInputFiles(file)
  await frame.getByTestId('target-node').click()
  await frame.getByRole('option', { name: 'Existing VM', exact: true }).click()
  await frame.getByTestId('admission-confirm').click()
  await expect(frame.getByTestId('admission-deployment')).toHaveValue(`ADDP_HOST_NODE_ID=${node}`)
  await frame.getByTestId('admission-collect').click()
  await expect(page).toHaveURL(new RegExp(`/monitor/monitoring-targets/new\\?node_id=${node}$`))
  await expect(frame.getByTestId('target-endpoint')).toHaveValue(manifest.collection.source.endpoint)
  expect(page.url()).not.toContain(new URL(manifest.collection.source.endpoint).hostname)
  await frame.getByTestId('target-save').click()
  await expect(page).toHaveURL(new RegExp(`/monitor/monitoring-targets/${id}$`))
  expect(state.writes).toHaveLength(1)
  expect(state.writes[0].input.subject.node_id).toBe(node)
  await page.reload()
  await expect(frame.getByTestId('target-endpoint')).toHaveValue(manifest.collection.source.endpoint)
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
