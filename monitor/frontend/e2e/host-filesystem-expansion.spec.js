import { expect, test } from '@playwright/test'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { filesystemMetrics, inodeMetrics } from '../src/utils/nodeResources'
import { resourceBackend, node, secondNode, identity, resourcePermissions, setIdentity, mount } from './node-resources.fixture'

const reads = state => state.reads.filter(row => row.path.endsWith('/resource_observations'))
const panel = page => page.getByTestId('resource-detail-expansion')
const button = (page, family = 'filesystem', id = node) => page.getByTestId(`resource-list-${family}-${id}`)
const table = page => panel(page).getByTestId('resource-filesystem-table')
const refresh = page => panel(page).getByTestId('resource-detail-refresh').click()
const groups = [filesystemMetrics, inodeMetrics].map(group => group.map(metric => metric.key).join(','))
const mounts = [mount, { ...mount, fstype: 'xfs' }, { ...mount, device: '/dev/other' }]

for (const locale of ['zh-cn', 'en']) test(`filesystem expansion preserves full mount identity, independent query cohorts and readable width (${locale})`, async ({ page }) => {
  const state = await resourceBackend(page, { locale, mounts, observationResponse: body => {
    if (!body.series[0].metric_key.startsWith('node.filesystem.inodes_')) return
    body.series = body.series.toReversed().map(series => {
      const index = mounts.findIndex(item => item.device === series.dimensions.device && item.fstype === series.dimensions.fstype)
      const values = { inodeUsedPercent: index * 25, inodeTotal: 100, inodeFree: 100 - index * 25, inodeUsed: index * 25 }
      const metric = inodeMetrics.find(metric => metric.key === series.metric_key)
      return { ...series, points: series.points.map(point => ({ ...point, value: values[metric.name] })) }
    })
  } })
  await page.goto('/node-resources?refresh=off')
  await expect(button(page)).toBeVisible()
  expect(reads(state)).toHaveLength(0)
  await button(page).click()
  await expect(table(page)).toContainText('50.00 %')
  const rows = table(page).locator('.el-table__body-wrapper tbody tr')
  await expect(rows).toHaveCount(3)
  for (let index = 0; index < mounts.length; index++) {
    const row = rows.nth(index)
    await expect(row.locator('td').nth(0)).toHaveText(mounts[index].device)
    await expect(row.locator('td').nth(1)).toHaveText('/data')
    await expect(row.locator('td').nth(2)).toHaveText(mounts[index].fstype)
    await expect(row.locator('strong')).toHaveText(['73.68 %', '100.00 GiB', '70.00 GiB', '25.00 GiB', '30.00 GiB', `${(index * 25).toFixed(2)} %`, `100 ${locale === 'en' ? 'inodes' : '个'}`, `${100 - index * 25} ${locale === 'en' ? 'inodes' : '个'}`, `${index * 25} ${locale === 'en' ? 'inodes' : '个'}`])
  }
  expect(reads(state).map(row => row.query)).toEqual(groups.map(metrics => ({ node_id: node, metrics })))
  await expect(panel(page).getByTestId('resource-detail-refresh')).toBeEnabled()
  const screenshots = mkdtempSync(join(tmpdir(), 'addp-host-filesystem-list-'))
  await panel(page).scrollIntoViewIfNeeded()
  await page.screenshot({ path: join(screenshots, `${locale}-wide.png`), animations: 'disabled' })
  await page.setViewportSize({ width: 620, height: 900 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  const bounds = await panel(page).boundingBox(), list = await page.getByTestId('resource-node-list').boundingBox()
  expect(bounds.width).toBeLessThanOrEqual(list.width)
  const refreshBounds = await panel(page).getByTestId('resource-detail-refresh').boundingBox()
  expect(refreshBounds.x).toBeGreaterThanOrEqual(list.x)
  expect(refreshBounds.x + refreshBounds.width).toBeLessThanOrEqual(list.x + list.width + 1)
  await panel(page).scrollIntoViewIfNeeded()
  const path = join(screenshots, `${locale}.png`)
  await page.screenshot({ path, animations: 'disabled' })
  await table(page).locator('.el-scrollbar__wrap').evaluate(element => { element.scrollLeft = element.scrollWidth })
  await page.screenshot({ path: join(screenshots, `${locale}-inodes.png`), animations: 'disabled' })
  console.log(`Host filesystem screenshots: ${screenshots}`)
  await button(page, 'filesystem', secondNode).click()
  await expect(table(page)).toContainText('50.00 %')
  expect(reads(state).slice(-2).map(row => row.query.node_id)).toEqual([secondNode, secondNode])
  await button(page, 'filesystem', secondNode).click()
  await expect(panel(page)).toHaveCount(0)
  expect(reads(state)).toHaveLength(4)
})

test('capacity and inode errors stay independent, clear old values and recover valid zero', async ({ page }) => {
  let wrongVersion = false
  const state = await resourceBackend(page, { observationResponse: body => {
    if (wrongVersion && body.series[0].metric_key.startsWith('node.filesystem.inodes_')) body.node_version++
  } })
  state.filesystemMode = 'budget'
  await page.goto('/node-resources?refresh=off')
  await button(page).click()
  await expect(panel(page).getByTestId('resource-filesystem-error')).toContainText('查询超过当前预算')
  await expect(table(page)).toContainText('20.00 %')
  await expect(table(page)).not.toContainText('100.00 GiB')
  state.filesystemMode = ''; state.inodeMode = 'unavailable'
  await refresh(page)
  await expect(panel(page).getByTestId('resource-inode-error')).toBeVisible()
  await expect(table(page)).toContainText('100.00 GiB')
  await expect(table(page)).not.toContainText('20.00 %')
  await expect(panel(page).getByTestId('resource-filesystem-error')).toHaveCount(0)
  state.inodeMode = ''; wrongVersion = true
  await refresh(page)
  await expect(panel(page).getByTestId('resource-inode-error')).toBeVisible()
  await expect(table(page)).toContainText('100.00 GiB')
  wrongVersion = false; state.inodeMode = 'inode-empty'
  await refresh(page)
  await expect(panel(page).getByTestId('resource-inode-error')).toHaveCount(0)
  await expect(table(page)).toContainText('0.00 %')
  await expect(page.getByTestId('resource-summary-memoryTotal').first()).toHaveText('16.00 GiB')
  expect(reads(state).map(row => row.query.metrics)).toEqual([...groups, ...groups, ...groups, ...groups])
})

test('missing inode values and unsupported filesystem remain explicit without fabricated zero', async ({ page }) => {
  const state = await resourceBackend(page)
  state.inodeMode = 'inode-missing'
  await page.goto('/node-resources?refresh=off')
  await button(page).click()
  await expect(table(page)).toContainText('缺失')
  await expect(table(page)).toContainText('73.68 %')
  await expect(table(page).locator('strong')).toHaveText(['73.68 %', '100.00 GiB', '70.00 GiB', '25.00 GiB', '30.00 GiB', '—', '—', '—', '—'])
  state.filesystemMode = 'filesystem-uncollected'; state.inodeMode = 'filesystem-uncollected'
  await refresh(page)
  await expect(panel(page).getByTestId('resource-filesystem-capability')).toBeVisible()
  await expect(table(page)).toHaveCount(0)
  await expect(panel(page).getByTestId('resource-filesystem-error')).toHaveCount(0)
})

test('collapse cancels the first cohort before inode query; switching during inode query drops late results', async ({ page }) => {
  let holdInodes = false
  const state = await resourceBackend(page, { holdInstant: true, observationResponse: body => {
    if (holdInodes && body.series[0].metric_key === filesystemMetrics[0].key) state.hold()
  } })
  await page.goto('/node-resources?refresh=off')
  await button(page).click()
  await expect.poll(() => reads(state).length).toBe(1)
  await button(page).click()
  state.release()
  await expect(panel(page)).toHaveCount(0)
  holdInodes = true
  await button(page).click()
  await expect.poll(() => reads(state).length).toBe(3)
  await button(page, 'network').click()
  await expect.poll(() => reads(state).length).toBe(4)
  holdInodes = false; state.release()
  await expect(panel(page).getByTestId('resource-network-table')).toContainText('eth0')
  await expect(table(page)).toHaveCount(0)
  expect(reads(state).map(row => row.query.metrics)).toEqual([groups[0], ...groups, 'node.network.receive_bytes_per_second,node.network.transmit_bytes_per_second'])
})

test('filesystem trend navigation keeps all mount dimensions and list context', async ({ page }) => {
  const state = await resourceBackend(page, { mounts })
  await page.goto('/node-resources?page=2&list_view=load-connections&refresh=off')
  await button(page).click()
  await expect(table(page)).toContainText('20.00 %')
  await table(page).getByRole('button', { name: '查看趋势', exact: true }).nth(1).click()
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  expect(Object.fromEntries(new URL(page.url()).searchParams)).toMatchObject({ ...mounts[1], metric: filesystemMetrics[0].key, page: '2', list_view: 'load-connections', refresh: 'off' })
  expect(state.reads.filter(row => row.path.endsWith('/resource_trends')).at(-1).query).toMatchObject({ ...mounts[1], metrics: filesystemMetrics[0].key })
  await page.getByRole('button', { name: '返回主机列表', exact: true }).click()
  await expect(button(page)).toBeVisible()
  await expect(panel(page)).toHaveCount(0)
})

test('automatic refresh retains the mount panel, reads only its two cohorts and pauses when hidden', async ({ page }) => {
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  await page.clock.install()
  const state = await resourceBackend(page)
  await page.goto('/node-resources')
  await button(page).click()
  await expect(table(page)).toContainText('20.00 %')
  await page.clock.fastForward(15001)
  await expect.poll(() => reads(state).length).toBe(4)
  await expect(table(page)).toContainText('20.00 %')
  expect(reads(state).map(row => row.query.metrics)).toEqual([...groups, ...groups])
  expect(reads(state).every(row => row.query.node_id === node)).toBe(true)
  await page.evaluate(() => { Object.defineProperty(document, 'hidden', { configurable: true, value: true }); document.dispatchEvent(new Event('visibilitychange')) })
  await page.clock.fastForward(60001)
  expect(reads(state)).toHaveLength(4)
  await page.evaluate(() => { Object.defineProperty(document, 'hidden', { configurable: true, value: false }); document.dispatchEvent(new Event('visibilitychange')) })
  await expect.poll(() => reads(state).length).toBe(6)
  await expect(table(page)).toContainText('20.00 %')
  await page.getByTestId('resource-list-view').getByText('负载与连接', { exact: true }).click()
  await expect(panel(page)).toHaveCount(0)
  await page.clock.fastForward(30001)
  expect(reads(state)).toHaveLength(6)
  expect(errors).toEqual([])
})

test('revocation during inode query rejects late results and inode denial stops the entire list', async ({ page }) => {
  await page.clock.install()
  let holdInodes = true
  const state = await resourceBackend(page, { observationResponse: body => {
    if (holdInodes && body.series[0].metric_key === filesystemMetrics[0].key) state.hold()
  } })
  await page.goto('/node-resources')
  await button(page).click()
  await expect.poll(() => reads(state).length).toBe(2)
  await setIdentity(page, identity(resourcePermissions, { type: 'tenant', tenant_id: '8' }))
  holdInodes = false; state.release()
  await expect(page.getByTestId('node-resources')).toHaveCount(0)
  await page.clock.fastForward(30001)
  expect(reads(state)).toHaveLength(2)
  await setIdentity(page, identity(resourcePermissions))
  await expect(button(page)).toBeVisible()
  await expect(panel(page)).toHaveCount(0)
  state.inodeMode = 'denied'
  await button(page).click()
  await expect(page.getByTestId('resource-error')).toBeVisible()
  await expect(panel(page)).toHaveCount(0)
  await expect(button(page)).toHaveCount(0)
  const before = state.reads.length
  await page.clock.fastForward(60001)
  expect(state.reads).toHaveLength(before)
})
