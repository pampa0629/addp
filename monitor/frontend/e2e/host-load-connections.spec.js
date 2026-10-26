import { expect, test } from '@playwright/test'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { hostListMetrics, hostLoadConnectionMetrics } from '../src/utils/nodeResources'
import { resourceBackend, identity, resourcePermissions, setIdentity } from './node-resources.fixture'

const metrics = hostLoadConnectionMetrics.map(metric => metric.key).join(',')
const reads = state => state.reads.filter(read => read.path.endsWith('/resource_summaries'))
const loadView = page => page.getByTestId('resource-list-view').getByRole('radio', { name: '负载与连接', exact: true })
const coreView = page => page.getByTestId('resource-list-view').getByRole('radio', { name: '核心资源', exact: true })
const refresh = page => page.getByRole('button', { name: '刷新', exact: true }).click()

for (const locale of ['zh-cn', 'en']) test(`twenty-host load and connection view uses one 80-series projection and local meaning (${locale})`, async ({ page }) => {
  const nodes = Array.from({ length: 20 }, (_, i) => ({ node_id: `12300000-0000-4000-8000-${String(i + 1).padStart(12, '0')}`, version: 1, display_name: `Host ${i + 1}`, enabled: true, addresses: [`192.0.2.${i + 1}`] }))
  const state = await resourceBackend(page, { locale, nodes, summaryResponse: body => {
    body.data.reverse()
    for (const row of body.data) {
      for (const line of row.series) line.points[0].value = line.unit === 'load' ? 10.59 : row.subject.node_id === nodes[0].node_id ? 0 : 42
      const tcp = row.series.find(line => line.unit === 'connections')
      if (row.subject.node_id === nodes[1].node_id) Object.assign(tcp.points[0], { value: null, sampled_at: null, data_state: 'no_data' })
    }
  } })
  await page.setViewportSize({ width: 1480, height: 900 })
  await page.goto('/node-resources?list_view=load-connections&refresh=off')
  const list = page.getByTestId('resource-node-list')
  await expect(list.locator('strong')).toHaveCount(80)
  await expect(list.getByTestId('resource-summary-load1m').first()).toHaveText(locale === 'en' ? 'About 10.59 tasks' : '约 10.59 个任务')
  await expect(list.getByTestId('resource-summary-tcpConnections').first()).toHaveText(locale === 'en' ? '0 connections' : '0 条')
  await expect(list.getByTestId('resource-summary-tcpConnections').nth(1)).toHaveText('—')
  await expect(list.getByTestId('resource-summary-tcpConnections').nth(2)).toHaveText(locale === 'en' ? '42 connections' : '42 条')
  await expect(list.locator('thead')).toContainText(locale === 'en' ? 'not a percentage' : '非百分比')
  await expect(list.locator('thead')).toContainText('ESTABLISHED + CLOSE_WAIT')
  expect(reads(state)).toHaveLength(1)
  expect(reads(state)[0].query).toEqual({ node_ids: nodes.map(row => row.node_id).join(','), metrics })
  expect(state.reads.find(read => read.path.endsWith('/host_nodes')).query.page_size).toBe('20')
  expect(state.reads.filter(read => /resource_observations|resource_trends/.test(read.path))).toHaveLength(0)
  const path = join(mkdtempSync(join(tmpdir(), 'addp-host-load-')), `${locale}.png`)
  await page.screenshot({ path, animations: 'disabled' }); console.log(`Host load screenshot: ${path}`)
  await page.setViewportSize({ width: 620, height: 900 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await expect(page.getByTestId('resource-list-view')).toBeVisible()
})

test('view switching retains committed context, restores detail return and reload, and reset retains the view', async ({ page }) => {
  const state = await resourceBackend(page)
  await page.goto('/node-resources?page=2&page_size=10&search=节点&node_kind=virtual&enabled=true&refresh=off')
  await expect(page.getByTestId('resource-summary-cores')).toHaveCount(2)
  await page.getByTestId('resource-search').fill('未提交草稿')
  await page.getByTestId('resource-list-view').getByText('负载与连接', { exact: true }).click()
  await expect.poll(() => reads(state).at(-1)?.query.metrics).toBe(metrics)
  let query = new URL(page.url()).searchParams
  expect(Object.fromEntries(query)).toEqual({ page: '2', page_size: '10', node_kind: 'virtual', enabled: 'true', list_view: 'load-connections', search: '节点', refresh: 'off' })
  expect(state.reads.filter(read => read.path.endsWith('/host_nodes')).at(-1).query.search).toBe('节点')
  await page.getByRole('button', { name: '查看资源', exact: true }).first().click()
  await expect(page.getByTestId('resource-cores')).toBeVisible()
  expect(new URL(page.url()).searchParams.get('list_view')).toBe('load-connections')
  expect(state.reads.filter(read => /resource_observations|resource_trends/.test(read.path)).every(read => !('list_view' in read.query))).toBe(true)
  await page.getByRole('button', { name: '返回主机列表', exact: true }).click()
  await expect(loadView(page)).toBeChecked()
  await page.reload()
  await expect(loadView(page)).toBeChecked()
  await expect(page.getByTestId('resource-summary-load5m')).toHaveCount(2)
  await page.getByTestId('resource-reset').click()
  await expect.poll(() => Object.fromEntries(new URL(page.url()).searchParams)).toEqual({ page_size: '10', list_view: 'load-connections', refresh: 'off' })
  await page.getByTestId('resource-list-view').getByText('核心资源', { exact: true }).click()
  await expect(page.getByTestId('resource-summary-cores')).toHaveCount(2)
  expect(new URL(page.url()).searchParams.has('list_view')).toBe(false)
  await expect.poll(() => reads(state).at(-1)?.query.metrics).toBe(hostListMetrics.map(metric => metric.key).join(','))
})

test('switching drops a late invalid core batch and errors clear only selected observations', async ({ page }) => {
  const state = await resourceBackend(page, { holdSummary: true, summaryMode: 'invalid' })
  await page.goto('/node-resources?refresh=off')
  await expect.poll(() => reads(state).length).toBe(1)
  state.summaryMode = ''
  await page.getByTestId('resource-list-view').getByText('负载与连接', { exact: true }).click()
  await expect.poll(() => reads(state).length).toBe(2)
  state.releaseSummary()
  await expect(page.getByTestId('resource-summary-load1m').first()).toContainText('约 0.00')
  await expect(page.getByTestId('resource-summary-error')).toHaveCount(0)
  state.summaryMode = 'budget'; await refresh(page)
  await expect(page.getByTestId('resource-summary-error')).toContainText('减少每页数量')
  expect(await page.getByTestId('resource-node-list').locator('strong').allTextContents()).toEqual(Array(8).fill('—'))
  await expect(page.getByRole('button', { name: '查看资源', exact: true })).toHaveCount(2)
  expect(reads(state)).toHaveLength(3)
  state.summaryMode = 'denied'; await refresh(page)
  await expect(page.getByTestId('resource-error')).toBeVisible()
  await expect(page.getByRole('button', { name: '查看资源', exact: true })).toHaveCount(0)
})

test('load view refresh remains scoped and identity changes reject late results', async ({ page }) => {
  await page.clock.install()
  const state = await resourceBackend(page)
  await page.goto('/node-resources?list_view=load-connections')
  await expect(page.getByTestId('resource-summary-load1m')).toHaveCount(2)
  await page.clock.fastForward(15001)
  await expect.poll(() => reads(state).length).toBe(2)
  expect(reads(state).every(read => read.query.metrics === metrics)).toBe(true)
  state.holdSummary()
  await refresh(page)
  await expect.poll(() => reads(state).length).toBe(3)
  await setIdentity(page, identity(resourcePermissions, { type: 'tenant', tenant_id: '8' }))
  state.releaseSummary()
  await expect(page.getByTestId('node-resources')).toHaveCount(0)
  await page.clock.fastForward(60001)
  expect(reads(state)).toHaveLength(3)
  await setIdentity(page, identity(resourcePermissions))
  await expect(loadView(page)).toBeChecked()
  await expect(page.getByTestId('resource-summary-load1m')).toHaveCount(2)
})

test('unreadable load view never requests resources', async ({ page }) => {
  const state = await resourceBackend(page, { identity: identity(['platform.host_node.read']) })
  await page.goto('/node-resources?list_view=load-connections')
  await expect(page.getByTestId('node-resources')).toHaveCount(0)
  expect(state.reads).toHaveLength(0)
})
