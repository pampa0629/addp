import { expect, test } from '@playwright/test'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { fixture, resourceIncident } from './platform-alerts.fixture'
import { node, identity, setIdentity, resourcePermissions, observations } from './node-resources.fixture'

const keys = ['monitor.platform_alert.read', ...resourcePermissions]
const historicalEnd = '2026-10-06T23:50:00.000Z'
for (const locale of ['zh-cn', 'en']) test(`saved trigger evidence and fixed trend form one navigable path (${locale})`, async ({ page }) => {
  const state = await fixture(page, { resources: true, keys, locale, entries: [structuredClone(resourceIncident)], path: `/platform-alerts?category=resource&subject_id=${node}&status=all` })
  const english = locale === 'en'
  await expect(page.getByTestId('pipeline-incidents')).toContainText('节点甲')
  await page.getByRole('button', { name: english ? 'Trigger evidence' : '触发依据', exact: true }).click()
  const evidence = page.getByTestId('resource-alert-evidence')
  await expect(evidence).toContainText('90.00 %')
  await expect(evidence).toContainText('81.00 %')
  await expect(evidence).toContainText('96.00 %')
  await expect(evidence).toContainText(english ? 'Above 80.00% continuously for 5 minutes' : '连续 5分钟 高于 80.00%')
  await page.setViewportSize({ width: 620, height: 900 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: join(tmpdir(), `addp-resource-alert-evidence-${locale}.png`), animations: 'disabled' })
  await page.getByRole('button', { name: english ? 'View trigger-period trend' : '查看触发时段趋势', exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/node-resources/${node}.*end=`))
  await expect(page.getByTestId('resource-history')).toBeVisible()
  const trendReads = () => state.resourceState.reads.filter(row => row.path.endsWith('/resource_trends'))
  await expect.poll(() => trendReads().length).toBeGreaterThan(0)
  expect(trendReads().at(-1).query).toMatchObject({ node_id: node, metrics: 'node.memory.used_percent', start: '2026-10-06T22:50:00.000Z', end: historicalEnd })
  const end = new URL(page.url()).searchParams.get('end')
  expect(end).toBe(historicalEnd)
  expect(new URL(page.url()).searchParams.get('refresh')).toBe('off')
  await expect(page.getByTestId('resource-refresh')).toHaveCount(0)
  await page.clock.install()
  const historicalReads = trendReads().length
  await page.clock.fastForward('00:01:01')
  expect(trendReads()).toHaveLength(historicalReads)
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  await page.screenshot({ path: join(tmpdir(), `addp-resource-alert-history-${locale}.png`), animations: 'disabled' })
  state.resourceState.serverEnd = '2026-10-07T00:15:00Z'
  await page.getByRole('button', { name: english ? 'Refresh' : '刷新', exact: true }).click()
  await expect.poll(() => trendReads().length).toBe(2)
  expect(trendReads().at(-1).query.end).toBe(historicalEnd)
  await page.getByTestId('resource-range').click()
  await page.getByRole('option', { name: english ? 'Last 5 minutes' : '近 5 分钟', exact: true }).click()
  await expect.poll(() => trendReads().length).toBe(3)
  expect(trendReads().at(-1).query).toMatchObject({ start: '2026-10-06T23:45:00.000Z', end: historicalEnd })
  await page.getByTestId('resource-live-trend').click()
  await expect(page.getByTestId('resource-history')).toHaveCount(0)
  await expect.poll(() => trendReads().at(-1)?.query.end).toBe('2026-10-07T00:15:00.000Z')
  expect(new URL(page.url()).searchParams.has('end')).toBe(false)
  await page.getByTestId('host-related-alerts').click()
  await expect(page.getByTestId('pipeline-incidents')).toBeVisible()
  expect(state.requests.filter(row => row.path.endsWith('/alerts')).at(-1).query).toMatchObject({ subject_id: node, category: 'resource', status: 'active' })
  expect(state.requests.some(row => row.path.includes('/resource-alert-rules'))).toBe(false)
})

test('alert-only readers see saved evidence without host requests or navigation', async ({ page }) => {
  const hostReads = []; page.on('request', req => { if (req.url().includes('/host_nodes')) hostReads.push(req.url()) })
  await fixture(page, { keys: ['monitor.platform_alert.read'], entries: [structuredClone(resourceIncident)] })
  await page.getByRole('button', { name: '触发依据', exact: true }).click()
  await expect(page.getByTestId('resource-alert-evidence')).toContainText('90.00 %')
  await expect(page.getByRole('button', { name: '查看触发时段趋势', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '查看当前主机资源', exact: true })).toHaveCount(0)
  expect(hostReads).toHaveLength(0)
  await setIdentity(page, identity([]))
  await expect(page.getByTestId('resource-alert-evidence')).toHaveCount(0)
  await expect(page.getByTestId('pipeline-incidents')).toHaveCount(0)
})

for (const evidence of [null, { ...resourceIncident.resource_evidence, latest: null }]) test(`missing or invalid evidence never becomes a reconstructed zero (${evidence === null ? 'missing' : 'invalid'})`, async ({ page }) => {
  await fixture(page, { resources: true, keys, entries: [{ ...structuredClone(resourceIncident), resource_evidence: evidence }] })
  await expect(page.getByTestId('pipeline-incidents')).toContainText('节点甲')
  await page.getByRole('button', { name: '触发依据', exact: true }).click()
  await expect(page.getByTestId('resource-alert-evidence')).toContainText('没有完整的触发依据')
  await expect(page.getByRole('button', { name: '查看触发时段趋势', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '查看当前主机资源', exact: true })).toBeVisible()
})

test('failed or late host reads cannot retain an old name or regain navigation', async ({ page }) => {
  await fixture(page, { resources: true, keys, entries: [structuredClone(resourceIncident)] })
  await expect(page.getByTestId('pipeline-incidents')).toContainText('节点甲')
  let held = false, release
  const pending = new Promise(resolve => { release = resolve })
  await page.route(`**/api/v1/system/platform/host_nodes/${node}`, async route => {
    held = true; await pending
    await route.fulfill({ json: { node_id: node, display_name: '迟到的旧主机', version: 1, addresses: [] } })
  })
  await page.locator('.platform-alerts').getByRole('button', { name: '刷新', exact: true }).click()
  await expect.poll(() => held).toBe(true)
  await expect(page.getByTestId('pipeline-incidents')).not.toContainText('节点甲')
  await setIdentity(page, identity(['monitor.platform_alert.read']))
  release()
  await expect(page.getByTestId('pipeline-incidents')).toContainText(node)
  await expect(page.getByTestId('pipeline-incidents')).not.toContainText('迟到的旧主机')
  await page.getByRole('button', { name: '触发依据', exact: true }).click()
  await expect(page.getByRole('button', { name: '查看触发时段趋势', exact: true })).toHaveCount(0)
})

for (const end of ['not-a-time', '2026-10-07T00:00:15.000Z']) test(`invalid historical end never silently requests a live curve (${end})`, async ({ page }) => {
  const state = await fixture(page, { resources: true, keys, entries: [] })
  await page.goto(`/node-resources/${node}?end=${encodeURIComponent(end)}`)
  await expect(page.getByText('历史结束时间无效，或晚于当前服务端查询时间。', { exact: true })).toBeVisible()
  expect(state.resourceState.reads.filter(row => row.path.endsWith('/resource_trends'))).toHaveLength(0)
})


test('a failed current host lookup removes navigation while leaving authorized opening evidence readable', async ({ page }) => {
  await fixture(page, { resources: true, keys, entries: [structuredClone(resourceIncident)] })
  await expect(page.getByTestId('pipeline-incidents')).toContainText('节点甲')
  await page.route(`**/api/v1/system/platform/host_nodes/${node}`, route => route.fulfill({ status: 404, json: { error: 'not found' } }))
  await page.locator('.platform-alerts').getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('pipeline-incidents')).toContainText(node)
  await expect(page.getByTestId('pipeline-incidents')).not.toContainText('节点甲')
  await page.getByRole('button', { name: '触发依据', exact: true }).click()
  await expect(page.getByTestId('resource-alert-evidence')).toContainText('当前无法读取此主机')
  await expect(page.getByTestId('resource-alert-evidence')).toContainText('90.00 %')
  await expect(page.getByRole('button', { name: '查看当前主机资源', exact: true })).toHaveCount(0)
})

test('host names alone never grant resource observation access', async ({ page }) => {
  const state = await fixture(page, { resources: true, keys: ['monitor.platform_alert.read', 'platform.host_node.read'], entries: [structuredClone(resourceIncident)] })
  await expect(page.getByTestId('pipeline-incidents')).toContainText('节点甲')
  await page.getByRole('button', { name: '触发依据', exact: true }).click()
  await expect(page.getByTestId('resource-alert-evidence')).toContainText('90.00 %')
  await expect(page.getByRole('button', { name: '查看触发时段趋势', exact: true })).toHaveCount(0)
  expect(state.resourceState.reads.filter(row => row.path.includes('/resource_'))).toHaveLength(0)
})

test('historical gaps stay empty and a response from another window is rejected', async ({ page }) => {
  const state = await fixture(page, { resources: true, keys, entries: [structuredClone(resourceIncident)] })
  await page.goto(`/node-resources/${node}?end=${historicalEnd}`)
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  await page.getByText('查看采样明细', { exact: true }).click()
  await expect(page.getByTestId('resource-evidence')).toContainText('缺失')
  await expect(page.getByTestId('resource-evidence').locator('tbody > tr').nth(1)).toContainText('—')
  await page.route('**/api/v1/monitor/platform/resource_trends?**', async route => {
    const response = observations(node, ['node.memory.used_percent'], true, '', state.resourceState.serverEnd)
    response.start = '2026-10-06T22:50:00.000Z'
    await route.fulfill({ json: response })
  })
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('resource-trend-error')).toBeVisible()
  await expect(page.getByTestId('resource-chart')).toHaveCount(0)
})
