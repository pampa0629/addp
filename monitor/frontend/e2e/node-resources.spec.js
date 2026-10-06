import { expect, test } from '@playwright/test'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { identity, node, resourceBackend, resourcePermissions, secondNode, setIdentity } from './node-resources.fixture'

test('lists owner nodes without fanout and opens eight metrics with server-anchored trends', async ({ page }) => {
  const state = await resourceBackend(page)
  await page.goto('/node-resources?page=2&search=节点')
  await expect(page.getByRole('button', { name: '查看资源' })).toHaveCount(2)
  expect(state.reads.filter(item => item.path.includes('resource_'))).toHaveLength(0)
  await page.getByRole('button', { name: '查看资源' }).first().click()
  await expect(page).toHaveURL(new RegExp(`/node-resources/${node}\\?page=2&search=`))
  await expect(page.getByTestId('resource-cores')).toContainText('0 核')
  await expect(page.getByTestId('resource-memoryAvailable')).toContainText('陈旧')
  await expect(page.getByTestId('resource-memoryAvailable')).toContainText('—')
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  const instant = state.reads.find(item => item.path.endsWith('resource_observations'))
  expect(instant.query.metrics.split(',')).toHaveLength(8)
  const trend = state.reads.find(item => item.path.endsWith('resource_trends'))
  expect(trend.query).toMatchObject({ node_id: node, metrics: 'node.memory.used_percent', start: '2026-10-06T23:00:00.000Z', end: '2026-10-07T00:00:00.000Z' })
  await page.getByText('查看采样明细', { exact: true }).click()
  await expect(page.getByTestId('resource-evidence')).toContainText('缺失')
  await page.screenshot({ path: join(tmpdir(), 'addp-node-resources-desktop.png'), animations: 'disabled' })
  await page.getByRole('button', { name: '返回节点列表' }).click()
  await expect(page.getByTestId('resource-search')).toHaveValue('节点')
})
test('canonical detail restores metric and range on reload; no endpoint or token enters URL', async ({ page }) => {
  const state = await resourceBackend(page)
  await page.goto(`/node-resources/${node}?range=7d&metric=node.load.average_1m&page=02&endpoint=secret`)
  await expect(page).toHaveURL(new RegExp(`/node-resources/${node}\\?range=7d&metric=node.load.average_1m$`))
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  expect(state.reads.find(item => item.path.endsWith('resource_trends')).query.start).toBe('2026-09-30T00:00:00.000Z')
  await page.reload()
  await expect(page.getByTestId('resource-metric')).toContainText('1 分钟平均负载')
  await expect(page.getByTestId('resource-range')).toContainText('近 7 天')
})
for (const mode of ['disabled', 'unconfigured', 'unavailable', 'timeout', 'budget', 'busy']) test(`${mode} refresh clears old metrics and has a distinct state`, async ({ page }) => {
  const state = await resourceBackend(page)
  await page.goto(`/node-resources/${node}`)
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  state.mode = mode
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('resource-error')).toBeVisible()
  await expect(page.getByTestId('resource-cores')).toHaveCount(0)
  await expect(page.getByTestId('resource-chart')).toHaveCount(0)
  expect(state.reads.filter(item => item.path.endsWith('resource_trends'))).toHaveLength(1)
})
for (const [name, actor] of [
  ['tenant', identity(resourcePermissions, { type: 'tenant', tenant_id: '9' })],
  ['service', identity(resourcePermissions, { type: 'platform' }, 'service_principal')],
  ['delegated', identity(resourcePermissions, { type: 'platform' }, 'user', { id: 'delegation' })],
  ['missing node permission', identity(['monitor.resource_observation.read'])],
  ['missing metric permission', identity(['platform.host_node.read'])]
]) test(`${name} cannot request node or resource data`, async ({ page }) => {
  const state = await resourceBackend(page, { identity: actor })
  await page.goto(`/node-resources/${node}`)
  await expect(page.getByTestId('node-resources')).toHaveCount(0)
  expect(state.reads).toHaveLength(0)
})
test('identity change clears the node and discards late observations', async ({ page }) => {
  const state = await resourceBackend(page, { holdInstant: true })
  await page.goto(`/node-resources/${node}`)
  await expect(page.getByRole('heading', { name: '节点甲' })).toBeVisible()
  await setIdentity(page, identity(resourcePermissions, { type: 'tenant', tenant_id: '8' }))
  state.release()
  await expect(page.getByTestId('node-resources')).toHaveCount(0)
  await setIdentity(page, identity(resourcePermissions))
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  expect(state.reads.filter(item => item.path.endsWith('resource_trends'))).toHaveLength(1)
})
test('navigation to another node discards the previous delayed response', async ({ page }) => {
  const state = await resourceBackend(page, { holdInstant: true })
  await page.goto(`/node-resources/${node}`)
  await expect.poll(() => state.reads.filter(item => item.path.endsWith('resource_observations')).length).toBe(1)
  await page.getByRole('button', { name: '返回节点列表' }).click()
  await page.getByRole('button', { name: '查看资源' }).nth(1).click()
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  state.release()
  await expect(page.getByRole('heading', { name: '节点乙' })).toBeVisible()
  expect(state.reads.filter(item => item.path.endsWith('resource_trends')).map(item => item.query.node_id)).toEqual([secondNode])
})
test('trend failure clears the previous chart while current readings remain independently valid', async ({ page }) => {
  const state = await resourceBackend(page)
  await page.goto(`/node-resources/${node}`)
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  state.trendMode = 'unavailable'
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('resource-trend-error')).toBeVisible()
  await expect(page.getByTestId('resource-cores')).toContainText('0 核')
  await expect(page.getByTestId('resource-chart')).toHaveCount(0)
})
test('disconnected points stay empty; English narrow layout has no horizontal overflow', async ({ page }) => {
  await resourceBackend(page, { locale: 'en', mode: 'disconnected' })
  await page.setViewportSize({ width: 620, height: 800 })
  await page.goto(`/node-resources/${node}`)
  await expect(page.getByTestId('resource-cores')).toContainText('Not connected')
  await expect(page.getByTestId('resource-cores')).toContainText('—')
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy()
  await page.screenshot({ path: join(tmpdir(), 'addp-node-resources-narrow.png'), animations: 'disabled' })
})
test('invalid node or mismatched response is an error instead of another node', async ({ page }) => {
  const state = await resourceBackend(page, { mode: 'invalid' })
  await page.goto('/node-resources/not-a-uuid')
  await expect(page.getByTestId('resource-error')).toContainText('节点标识无效')
  expect(state.reads).toHaveLength(0)
  await page.goto(`/node-resources/${node}`)
  await expect(page.getByTestId('resource-error')).toContainText('资源响应不完整')
  await expect(page.getByTestId('resource-cores')).toHaveCount(0)
})
