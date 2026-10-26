import { expect, test } from '@playwright/test'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { diskMetrics, networkMetrics } from '../src/utils/nodeResources'
import { resourceBackend, node, secondNode, identity, resourcePermissions, setIdentity } from './node-resources.fixture'

const deviceReads = state => state.reads.filter(row => row.path.endsWith('/resource_observations'))
const summaryReads = state => state.reads.filter(row => row.path.endsWith('/resource_summaries'))
const button = (page, family, id = node) => page.getByTestId(`resource-list-${family}-${id}`)
const panel = page => page.getByTestId('resource-detail-expansion')
const point = (value, data_state = 'valid') => ({ value, data_state })
const disks = [
  { device: 'nbd10', points: { diskRead: point(0), diskWrite: point(0), diskBusy: point(0) } },
  { device: 'vda', points: { diskRead: point(2048), diskWrite: point(4096), diskBusy: point(0.5) } },
  { device: 'lost', points: { diskRead: point(null, 'no_data'), diskWrite: point(null, 'no_data'), diskBusy: point(null, 'no_data') } }
]

for (const locale of ['zh-cn', 'en']) test(`list loads only one chosen host and family, reusing device units and idle expansion (${locale})`, async ({ page }) => {
  const state = await resourceBackend(page, { locale, diskDevices: disks })
  await page.goto('/node-resources?refresh=off')
  await expect(page.getByRole('button', { name: locale === 'en' ? 'View resources' : '查看资源', exact: true })).toHaveCount(2)
  expect(deviceReads(state)).toHaveLength(0)
  await button(page, 'disk').click()
  const table = panel(page).getByTestId('resource-disk-table')
  await expect(table).toContainText('2.00 KiB/s')
  await expect(table).toContainText('4.00 KiB/s')
  await expect(table).toContainText('0.50 %')
  const names = table.locator('.el-table__body-wrapper tbody tr td:first-child .cell')
  await expect(names).toHaveText(['vda', 'lost'])
  await expect(button(page, 'disk')).toHaveAttribute('aria-expanded', 'true')
  expect(deviceReads(state)).toHaveLength(1)
  expect(deviceReads(state)[0].query).toEqual({ node_id: node, metrics: diskMetrics.map(metric => metric.key).join(',') })
  await panel(page).getByTestId('resource-disk-toggle').click()
  await expect(names).toHaveText(['vda', 'nbd10', 'lost'])
  await expect(table).toContainText('0.00 B/s')
  expect(deviceReads(state)).toHaveLength(1)
  await page.setViewportSize({ width: 620, height: 900 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  const panelBounds = await panel(page).boundingBox(), listBounds = await page.getByTestId('resource-node-list').boundingBox()
  expect(panelBounds.width).toBeLessThanOrEqual(listBounds.width)
  const refreshBounds = await panel(page).getByTestId('resource-detail-refresh').boundingBox()
  expect(refreshBounds.x).toBeGreaterThanOrEqual(listBounds.x)
  expect(refreshBounds.x + refreshBounds.width).toBeLessThanOrEqual(listBounds.x + listBounds.width + 1)
  const path = join(mkdtempSync(join(tmpdir(), 'addp-host-device-list-')), `${locale}.png`)
  await page.screenshot({ path, animations: 'disabled' }); console.log(`Host device expansion screenshot: ${path}`)
  await button(page, 'network').click()
  await expect(panel(page).getByTestId('resource-network-table')).toContainText('eth0')
  await expect(panel(page).getByTestId('resource-disk-table')).toHaveCount(0)
  await expect(button(page, 'disk')).toHaveAttribute('aria-expanded', 'false')
  expect(deviceReads(state).at(-1).query).toEqual({ node_id: node, metrics: networkMetrics.map(metric => metric.key).join(',') })
  await button(page, 'network', secondNode).click()
  await expect(panel(page)).toHaveCount(1)
  await expect.poll(() => deviceReads(state).at(-1).query.node_id).toBe(secondNode)
  await expect(button(page, 'network')).toHaveAttribute('aria-expanded', 'false')
  await button(page, 'network', secondNode).click()
  await expect(panel(page)).toHaveCount(0)
  expect(deviceReads(state)).toHaveLength(3)
  expect(summaryReads(state)).toHaveLength(1)
  expect(state.reads.filter(row => row.path.endsWith('/resource_trends'))).toHaveLength(0)
  expect(page.url()).not.toMatch(/device=|family=|expanded/)
})

test('default row arrow opens only one host and closing it makes no extra request', async ({ page }) => {
  const state = await resourceBackend(page)
  await page.goto('/node-resources?refresh=off')
  const arrows = page.getByTestId('resource-node-list').locator('.el-table__expand-icon')
  await arrows.first().click()
  await expect(panel(page).getByTestId('resource-disk-table')).toBeVisible()
  await arrows.nth(1).click()
  await expect(panel(page)).toHaveCount(1)
  await expect.poll(() => deviceReads(state).at(-1).query.node_id).toBe(secondNode)
  await arrows.nth(1).click()
  await expect(panel(page)).toHaveCount(0)
  expect(deviceReads(state)).toHaveLength(2)
})

test('switching family rejects a late invalid old observation, and a trend keeps exact host and device', async ({ page }) => {
  const state = await resourceBackend(page, { holdInstant: true })
  state.diskMode = 'invalid'
  await page.goto('/node-resources?page=2&list_view=load-connections&refresh=off')
  await button(page, 'disk').click()
  await expect.poll(() => deviceReads(state).length).toBe(1)
  await button(page, 'network').click()
  await expect.poll(() => deviceReads(state).length).toBe(2)
  state.release()
  await expect(panel(page).getByTestId('resource-network-table')).toContainText('2.00 KiB/s')
  await expect(panel(page).getByTestId('resource-disk-error')).toHaveCount(0)
  await expect(panel(page).getByTestId('resource-network-error')).toHaveCount(0)
  state.diskMode = ''
  await panel(page).getByRole('button', { name: '查看趋势', exact: true }).click()
  await expect(page.getByTestId('resource-selected-device')).toHaveText('eth0')
  expect(new URL(page.url()).pathname).toBe(`/node-resources/${node}`)
  expect(Object.fromEntries(new URL(page.url()).searchParams)).toMatchObject({ page: '2', list_view: 'load-connections', metric: networkMetrics[0].key, device: 'eth0', refresh: 'off' })
  await page.getByRole('button', { name: '返回主机列表', exact: true }).click()
  await expect(panel(page)).toHaveCount(0)
  await expect(page.getByTestId('resource-list-view').getByRole('radio', { name: '负载与连接', exact: true })).toBeChecked()
})

test('budget, malformed version and partial missing data clear old values while authorized summaries remain', async ({ page }) => {
  let wrongVersion = false, partial = false
  const state = await resourceBackend(page, { observationResponse: body => {
    if (wrongVersion) body.node_version++
    if (partial && body.series[0].metric_key.startsWith('node.network.')) Object.assign(body.series[0].points[0], { value: null, sampled_at: null, data_state: 'no_data' })
  } })
  await page.goto('/node-resources?refresh=off')
  state.networkMode = 'budget'
  await button(page, 'network').click()
  await expect(panel(page).getByTestId('resource-network-error')).toBeVisible()
  await expect(panel(page).getByTestId('resource-network-table')).toHaveCount(0)
  await expect(page.getByTestId('resource-summary-memoryTotal').first()).toHaveText('16.00 GiB')
  state.networkMode = ''; wrongVersion = true
  await panel(page).getByTestId('resource-detail-refresh').click()
  await expect(panel(page).getByTestId('resource-network-error')).toBeVisible()
  wrongVersion = false; partial = true
  await panel(page).getByTestId('resource-detail-refresh').click()
  const values = panel(page).getByTestId('resource-network-table').locator('strong')
  await expect(values).toHaveText(['—', '2.00 KiB/s'])
  await expect(panel(page).getByTestId('resource-network-error')).toHaveCount(0)
  state.networkMode = 'unavailable'
  await panel(page).getByTestId('resource-detail-refresh').click()
  await expect(panel(page).getByTestId('resource-network-error')).toBeVisible()
  await expect(panel(page).getByTestId('resource-network-table')).toHaveCount(0)
  expect(summaryReads(state)).toHaveLength(1)
})

test('uncollected network and stopped hosts preserve explicit unknown states', async ({ page }) => {
  const state = await resourceBackend(page)
  state.networkMode = 'network-uncollected'
  await page.goto('/node-resources?refresh=off')
  await button(page, 'network').click()
  await expect(panel(page).getByTestId('resource-network-empty')).toContainText('未提供网络接口指标')
  await expect(panel(page).getByTestId('resource-network-table')).toHaveCount(0)
  state.networkMode = 'network-idle'
  await panel(page).getByTestId('resource-detail-refresh').click()
  await expect(panel(page).getByTestId('resource-network-table').locator('strong')).toHaveText(['0.00 B/s', '0.00 B/s'])
  state.networkMode = 'disconnected'
  await panel(page).getByTestId('resource-detail-refresh').click()
  await expect(panel(page).getByTestId('resource-network-table')).toHaveCount(0)
  await expect(panel(page).getByTestId('resource-network-empty')).toBeVisible()
})

test('list refresh preserves one expansion, query changes and reload close it without prefetch', async ({ page }) => {
  const state = await resourceBackend(page)
  await page.goto('/node-resources?page=2&refresh=off')
  await button(page, 'disk').click()
  await expect(panel(page).getByTestId('resource-disk-table')).toBeVisible()
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(panel(page).getByTestId('resource-disk-table')).toBeVisible()
  await expect.poll(() => deviceReads(state).length).toBe(2)
  await page.getByTestId('resource-list-view').getByText('负载与连接', { exact: true }).click()
  await expect(panel(page)).toHaveCount(0)
  expect(deviceReads(state)).toHaveLength(2)
  await button(page, 'network').click()
  await expect(panel(page).getByTestId('resource-network-table')).toBeVisible()
  await page.reload()
  await expect(button(page, 'network')).toBeVisible()
  await expect(panel(page)).toHaveCount(0)
  expect(deviceReads(state)).toHaveLength(3)
})

test('only the expanded family refreshes, hidden pages pause, and a removed current host closes its panel', async ({ page }) => {
  await page.clock.install()
  const state = await resourceBackend(page)
  await page.goto('/node-resources')
  await button(page, 'network').click()
  await expect(panel(page).getByTestId('resource-network-table')).toBeVisible()
  await page.clock.fastForward(15001)
  await expect.poll(() => deviceReads(state).length).toBe(2)
  await expect.poll(() => summaryReads(state).length).toBe(2)
  await expect(panel(page).getByTestId('resource-network-table')).toBeVisible()
  expect(deviceReads(state).every(row => row.query.node_id === node && row.query.metrics === networkMetrics.map(metric => metric.key).join(','))).toBe(true)
  await page.evaluate(() => { Object.defineProperty(document, 'hidden', { configurable: true, value: true }); document.dispatchEvent(new Event('visibilitychange')) })
  const before = deviceReads(state).length
  await page.clock.fastForward(60001)
  expect(deviceReads(state)).toHaveLength(before)
  state.listNodes = [{ node_id: secondNode, version: 1, enabled: true, display_name: 'Only B', addresses: [] }]
  await page.evaluate(() => { Object.defineProperty(document, 'hidden', { configurable: true, value: false }); document.dispatchEvent(new Event('visibilitychange')) })
  await expect(panel(page)).toHaveCount(0)
  await expect(button(page, 'network')).toHaveCount(0)
  const after = deviceReads(state).length
  await page.clock.fastForward(30001)
  expect(deviceReads(state)).toHaveLength(after)
})

test('scope revocation drops held results and a device denial stops further reads', async ({ page }) => {
  await page.clock.install()
  const state = await resourceBackend(page, { holdInstant: true })
  await page.goto('/node-resources')
  await button(page, 'disk').click()
  await expect.poll(() => deviceReads(state).length).toBe(1)
  await setIdentity(page, identity(resourcePermissions, { type: 'tenant', tenant_id: '8' }))
  state.release()
  await expect(page.getByTestId('node-resources')).toHaveCount(0)
  await page.clock.fastForward(30001)
  expect(deviceReads(state)).toHaveLength(1)
  await setIdentity(page, identity(resourcePermissions))
  await expect(button(page, 'network')).toBeVisible()
  await expect(panel(page)).toHaveCount(0)
  state.networkMode = 'denied'
  await button(page, 'network').click()
  await expect(page.getByTestId('resource-error')).toBeVisible()
  await expect(panel(page)).toHaveCount(0)
  await expect(button(page, 'disk')).toHaveCount(0)
  const before = state.reads.length
  await page.clock.fastForward(60001)
  expect(state.reads).toHaveLength(before)
})
