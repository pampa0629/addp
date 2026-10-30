import { expect, test } from '@playwright/test'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { identity, node, resourceBackend, resourcePermissions, setIdentity, mount } from './node-resources.fixture'
import { trackResourceChart, resourceChartProps, resourceChartState, dragResourceChartZoom } from './resource-chart.fixture'

test.beforeEach(({ page }) => trackResourceChart(page))

function completeTrend(body, params, comparing = false, gapKey = '') {
  const start = Date.parse(params.get('start')), end = Date.parse(params.get('end')), width = end - start
  const step = Math.max(15, Math.ceil(width / 1000 / 999 / 15) * 15)
  body.step_seconds = step
  for (const line of body.series) line.points = Array.from({ length: Math.floor(width / (step * 1000)) + 1 }, (_, index) => {
    const at = start + index * step * 1000, stamp = new Date(at).toISOString()
    const usage = comparing ? at <= start + width / 2 ? 30 : 40 : index === 0 ? 0 : 50
    const key = line.metric_key
    const value = key === 'node.memory.total_bytes' ? 64 * 1024 ** 3 : key === 'node.memory.available_bytes' ? 64 * 1024 ** 3 * (1 - usage / 100)
      : key === 'node.cpu.logical_cores' ? 18 : line.unit === 'percent' ? usage : line.unit === 'bytes' ? 8 * 1024 ** 3 : line.unit === 'seconds' ? 3600 + index * step : line.unit === 'inodes' ? 1000 + index : line.unit === 'bytes_per_second' ? (key.includes('read_') ? index === 0 ? 0 : 1 : 2) * 1024 ** 2 : 2.5
    const missing = gapKey === key && index === 1
    return { evaluated_at: stamp, sampled_at: missing ? null : stamp, value: missing ? null : value, data_state: missing ? 'no_data' : 'valid' }
  })
  body.series.reverse()
}
const trendReads = state => state.reads.filter(read => read.path.endsWith('resource_trends'))
const refresh = (page, locale = 'zh-cn') => page.getByRole('button', { name: locale === 'en' ? 'Refresh' : '刷新', exact: true }).click()

for (const locale of ['zh-cn', 'en']) for (const [family, key, names, keys, alternate, dimensions = {}] of [
  ['cpu', 'node.cpu.busy_percent', ['cpuBusy'], ['node.cpu.busy_percent'], null],
  ['memory', 'node.memory.used_percent', ['memoryTotal', 'memoryAvailable', 'memoryUsed'], ['node.memory.total_bytes', 'node.memory.available_bytes', 'node.memory.used_percent'], ['node.memory.available_bytes', '可用内存', 'Available memory']],
  ['disk', 'node.disk.read_bytes_per_second', ['diskRead', 'diskWrite'], ['node.disk.read_bytes_per_second', 'node.disk.write_bytes_per_second'], ['node.disk.write_bytes_per_second', '磁盘写入吞吐', 'Disk write throughput'], { device: 'sda' }],
  ['load', 'node.load.average_1m', ['load1m'], ['node.load.average_1m'], null],
  ['uptime', 'node.uptime_seconds', ['uptime'], ['node.uptime_seconds'], null],
  ['filesystem-percent', 'node.filesystem.used_percent', ['filesystemUsedPercent'], ['node.filesystem.used_percent'], null, mount],
  ['filesystem-capacity', 'node.filesystem.total_bytes', ['filesystemTotal'], ['node.filesystem.total_bytes'], null, mount],
  ['inode-percent', 'node.filesystem.inodes_used_percent', ['inodeUsedPercent'], ['node.filesystem.inodes_used_percent'], null, mount],
  ['inode-count', 'node.filesystem.inodes_total', ['inodeTotal'], ['node.filesystem.inodes_total'], null, mount],
]) test(`${family} time zoom preserves full samples and scope through refresh (${locale})`, async ({ page }) => {
  let gap = false
  const state = await resourceBackend(page, { locale, trendResponse: (body, params) => completeTrend(body, params, false, gap ? key : '') })
  await page.setViewportSize({ width: 620, height: 900 })
  await page.goto(`/node-resources/${node}?metric=${key}&range=7d&refresh=off&${new URLSearchParams(dimensions)}`)
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  const help = page.getByTestId('resource-trend-help').getByRole('button')
  await help.focus()
  await expect(page.getByRole('tooltip')).toContainText(locale === 'en' ? 'period comparisons keep their original scope' : '时段对比的范围保持不变')
  await help.blur()
  const before = trendReads(state).length, params = trendReads(state).at(-1).query
  expect(params.metrics).toBe(keys.join(','))
  expect(params).toMatchObject(dimensions)
  const props = await resourceChartProps(page)
  expect(props.zoomable).toBe(true)
  expect(props.config.chart_type).toBe('line')
  expect(props.rows).toHaveLength(984)
  expect(props.config.measures).toEqual(names)
  if (family === 'load') expect(props.rows.at(-1).load1m).toBe(2.5)
  if (family === 'uptime') {
    expect(props.rows.at(-1).uptime).toBe((3600 + 983 * 615) / 86400)
    expect(props.config.field_presentations.at(-1).unit).toBe(locale === 'en' ? 'days' : '天')
  }
  if (family === 'filesystem-capacity') {
    expect(props.rows.at(-1).filesystemTotal).toBe(8)
    expect(props.config.field_presentations.at(-1).unit).toBe('GiB')
  }
  if (family === 'inode-count') expect(props.rows.at(-1).inodeTotal).toBe(1983)
  if (family === 'memory') {
    expect(props.valueAxes).toHaveLength(2)
    expect(props.valueAxes[1]).toMatchObject({ min: 0, max: 100 })
    expect(props.rows.at(-1)).toMatchObject({ memoryTotal: 64, memoryAvailable: 32, memoryUsed: 50 })
  }
  await expect.poll(async () => (await resourceChartState(page)).finished).toBe(true)
  await dragResourceChartZoom(page, 0.8)
  await expect.poll(async () => (await resourceChartState(page)).start).toBeGreaterThan(70)
  const start = (await resourceChartState(page)).start
  expect((await resourceChartState(page)).lengths).toEqual(names.map(() => 984))
  expect((await resourceChartProps(page)).rows).toEqual(props.rows)
  expect(trendReads(state)).toHaveLength(before)
  await page.evaluate(() => document.documentElement.style.setProperty('--addp-border-color', 'rgb(30, 40, 50)'))
  await expect.poll(async () => (await resourceChartState(page)).borderColor).toBe('rgb(30, 40, 50)')
  expect((await resourceChartState(page)).start).toBeCloseTo(start, 2)
  const layoutFits = () => page.locator('.chart-renderer').evaluate(element =>
    element.querySelector('canvas').clientWidth === element.clientWidth && document.documentElement.scrollWidth <= innerWidth)
  await page.setViewportSize({ width: 900, height: 900 })
  await expect.poll(layoutFits).toBe(true)
  await page.setViewportSize({ width: 620, height: 900 })
  await expect.poll(layoutFits).toBe(true)
  expect((await resourceChartState(page)).start).toBeCloseTo(start, 2)
  state.serverEnd = '2026-10-07T00:00:15Z'
  gap = true
  await refresh(page, locale)
  await expect.poll(() => trendReads(state).at(-1).query.end).toBe('2026-10-07T00:00:15.000Z')
  await expect.poll(async () => (await resourceChartProps(page)).rows[0].evaluated_at).toBe('2026-09-30T00:00:15.000Z')
  expect((await resourceChartState(page)).start).toBeCloseTo(start, 2)
  expect((await resourceChartProps(page)).rows[1][names[keys.indexOf(key)]]).toBeNull()
  if (alternate) {
    await page.getByTestId('resource-metric').click()
    await page.getByRole('option', { name: alternate[locale === 'en' ? 2 : 1], exact: true }).click()
    await expect(page).toHaveURL(url => url.searchParams.get('metric') === alternate[0])
    await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
    expect((await resourceChartState(page)).start).toBeCloseTo(start, 2)
  }
  await expect.poll(async () => (await resourceChartState(page)).finished).toBe(true)
  await page.getByTestId('resource-chart').scrollIntoViewIfNeeded()
  const screenshot = join(mkdtempSync(join(tmpdir(), 'addp-host-zoom-')), `${family}-${locale}.png`)
  await page.getByTestId('resource-chart').screenshot({ path: screenshot, animations: 'disabled' })
  console.log(`Host zoom screenshot: ${screenshot}`)
  await page.getByTestId('resource-range').click()
  await page.getByRole('option', { name: locale === 'en' ? 'Last 5 minutes' : '近 5 分钟', exact: true }).click()
  await expect.poll(async () => (await resourceChartProps(page)).rows.length).toBe(21)
  expect((await resourceChartState(page)).start).toBe(0)
  state.trendMode = 'budget'
  await refresh(page, locale)
  await expect(page.getByTestId('resource-trend-error')).toBeVisible()
  await expect(page.getByTestId('resource-chart')).toHaveCount(0)
  state.trendMode = ''
  await refresh(page, locale)
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  expect((await resourceChartState(page)).start).toBe(0)
})

for (const key of ['node.cpu.busy_percent', 'node.memory.used_percent']) test(`zoom never recomputes period comparison or samples (${key})`, async ({ page }) => {
  const state = await resourceBackend(page, { trendResponse: (body, params) => completeTrend(body, params, true) })
  await page.goto(`/node-resources/${node}?metric=${key}&range=5m&compare=previous&refresh=off`)
  await expect(page.getByTestId('resource-compare-change')).toHaveText('较上期上升 10.00 个百分点')
  const count = trendReads(state).length, props = await resourceChartProps(page)
  expect(props.rows).toHaveLength(20)
  await dragResourceChartZoom(page, 0.6)
  await expect.poll(async () => (await resourceChartState(page)).start).toBeGreaterThan(50)
  expect((await resourceChartProps(page)).rows).toEqual(props.rows)
  expect(trendReads(state)).toHaveLength(count)
  await expect(page.getByTestId('resource-compare-current').locator('strong')).toHaveText('40.00 %')
  await expect(page.getByTestId('resource-compare-previous').locator('strong')).toHaveText('30.00 %')
  await page.getByText('查看采样明细', { exact: true }).click()
  await expect(page.getByTestId('resource-evidence').locator('tbody > tr')).toHaveCount(20)
  await page.getByTestId('resource-compare').uncheck()
  await expect(page.getByTestId('resource-comparison')).toHaveCount(0)
  await expect.poll(async () => (await resourceChartProps(page)).rows.length).toBe(21)
  expect((await resourceChartState(page)).start).toBe(0)
})

test('automatic refresh retains scalar zoom while a new metric or identity resets it', async ({ page }) => {
  await page.clock.install()
  const state = await resourceBackend(page, { trendResponse: completeTrend })
  await page.goto(`/node-resources/${node}?metric=node.cpu.busy_percent&range=5m&refresh=10`)
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  await dragResourceChartZoom(page, 0.6)
  await expect.poll(async () => (await resourceChartState(page)).start).toBeGreaterThan(50)
  const start = (await resourceChartState(page)).start, count = trendReads(state).length
  state.serverEnd = '2026-10-07T00:00:15Z'
  await page.clock.fastForward(10001)
  await expect.poll(() => trendReads(state).length).toBe(count + 1)
  await expect.poll(async () => (await resourceChartProps(page)).rows[0].evaluated_at).toBe('2026-10-06T23:55:15.000Z')
  expect((await resourceChartState(page)).start).toBeCloseTo(start, 2)
  await page.getByTestId('resource-metric').click()
  await page.getByRole('option', { name: 'CPU 逻辑核数', exact: true }).click()
  await expect(page).toHaveURL(/node.cpu.logical_cores/)
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  expect((await resourceChartState(page)).start).toBe(0)
  await dragResourceChartZoom(page, 0.6)
  await expect.poll(async () => (await resourceChartState(page)).start).toBeGreaterThan(50)
  state.identity = identity([])
  await setIdentity(page, state.identity)
  await expect(page.getByTestId('resource-chart')).toHaveCount(0)
  state.identity = identity(resourcePermissions)
  await setIdentity(page, state.identity)
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  expect((await resourceChartState(page)).start).toBe(0)
  await page.getByTestId('resource-metric').click()
  await page.getByRole('option', { name: '系统平均负载（1 分钟）', exact: true }).click()
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  expect((await resourceChartProps(page)).zoomable).toBe(true)
  expect((await resourceChartState(page)).start).toBe(0)
  await expect(page.getByTestId('resource-trend-help').getByRole('button')).toHaveAccessibleName(/时段对比的范围保持不变/)
  await dragResourceChartZoom(page, 0.6)
  await expect.poll(async () => (await resourceChartState(page)).start).toBeGreaterThan(50)
  const loadZoom = (await resourceChartState(page)).start, loadCount = trendReads(state).length
  state.serverEnd = '2026-10-07T00:00:30Z'
  await page.clock.fastForward(10001)
  await expect.poll(() => trendReads(state).length).toBe(loadCount + 1)
  await expect.poll(async () => (await resourceChartProps(page)).rows[0].evaluated_at).toBe('2026-10-06T23:55:30.000Z')
  expect((await resourceChartState(page)).start).toBeCloseTo(loadZoom, 2)
})


test('filesystem zoom stays with the full mount identity and resets across metrics', async ({ page }) => {
  const secondMount = { device: '/dev/archive', mountpoint: '/archive', fstype: 'xfs' }
  const state = await resourceBackend(page, { mounts: [mount, secondMount], trendResponse: completeTrend })
  await page.goto(`/node-resources/${node}?range=5m&refresh=off`)
  const selectMount = path => page.getByTestId('resource-filesystem-table').getByRole('row').filter({ hasText: path }).getByRole('button', { name: '查看趋势', exact: true }).click()
  await selectMount('/data')
  await expect(page.getByTestId('resource-selected-mount')).toContainText('/data')
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  await dragResourceChartZoom(page, 0.6)
  await expect.poll(async () => (await resourceChartState(page)).start).toBeGreaterThan(50)
  const zoom = (await resourceChartState(page)).start
  await refresh(page)
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  expect((await resourceChartState(page)).start).toBeCloseTo(zoom, 2)
  await selectMount('/archive')
  await expect(page.getByTestId('resource-selected-mount')).toContainText('/archive')
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  expect(trendReads(state).at(-1).query).toMatchObject(secondMount)
  expect((await resourceChartState(page)).start).toBe(0)
  await dragResourceChartZoom(page, 0.6)
  await expect.poll(async () => (await resourceChartState(page)).start).toBeGreaterThan(50)
  await page.getByTestId('resource-metric').click()
  await page.getByRole('option', { name: '总容量', exact: true }).click()
  await expect(page).toHaveURL(url => url.searchParams.get('metric') === 'node.filesystem.total_bytes' && url.searchParams.get('mountpoint') === '/archive')
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  expect((await resourceChartState(page)).start).toBe(0)
  expect(trendReads(state).at(-1).query).toMatchObject({ ...secondMount, metrics: 'node.filesystem.total_bytes' })
  await page.getByTestId('resource-metric').click()
  await page.getByRole('option', { name: 'inode 总数', exact: true }).click()
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  expect((await resourceChartState(page)).start).toBe(0)
  expect(trendReads(state).at(-1).query).toMatchObject({ ...secondMount, metrics: 'node.filesystem.inodes_total' })
  await dragResourceChartZoom(page, 0.6)
  await expect.poll(async () => (await resourceChartState(page)).start).toBeGreaterThan(50)
  await selectMount('/data')
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  expect(trendReads(state).at(-1).query).toMatchObject(mount)
  expect((await resourceChartState(page)).start).toBe(0)
})
