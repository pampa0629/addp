import { expect, test } from '@playwright/test'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { identity, node, resourceBackend, setIdentity, mount } from './node-resources.fixture'
import { trackResourceChart, resourceChartProps, resourceChartState, dragResourceChartZoom,
  clickResourceChartLegend, hoverResourceChartPoint, resourceChartTooltip } from './resource-chart.fixture'

test.use({ timezoneId: 'UTC' })
test.beforeEach(({ page }) => trackResourceChart(page))

function interactionTrend(body, params, comparison = false) {
  const start = Date.parse(params.get('start')), width = Date.parse(params.get('end')) - start
  const step = Math.max(15, Math.ceil(width / 1000 / 999 / 15) * 15)
  body.step_seconds = step
  for (const line of body.series) line.points = Array.from({ length: Math.floor(width / (step * 1000)) + 1 }, (_, index) => {
    const at = start + index * step * 1000, stamp = new Date(at).toISOString(), key = line.metric_key
    const percent = comparison ? at <= start + width / 2 ? 30 : 40 : index === 0 ? 0 : index + 0.25
    const value = key === 'node.memory.total_bytes' ? 64 * 1024 ** 3 : key === 'node.memory.available_bytes' ? (32 + index / 4) * 1024 ** 3
      : line.unit === 'load' || line.unit === 'milliseconds' ? index === 0 ? 0 : index / 4 + 0.25
      : line.unit === 'cores' ? index === 0 ? 0 : 18
      : line.unit === 'seconds' ? index === 0 ? 0 : 3600 + index * step
      : line.unit === 'inodes' ? index === 0 ? 0 : 1000 + index
      : line.unit === 'percent' ? percent : (key.includes('write_') || key.includes('transmit_') ? 2 + index / 4 : index / 4) * 1024 ** 2
    const data_state = !comparison && index === 1 && (key.includes('available_') || !key.startsWith('node.memory.')) ? 'no_data'
      : !comparison && index === 2 && (key === 'node.disk.io_busy_percent' || line.unit === 'milliseconds' || key === 'node.cpu.logical_cores' || key.startsWith('node.load.') || key.startsWith('node.filesystem.') || key === 'node.uptime_seconds' || key.endsWith('used_percent') || key.includes('write_') || key.includes('transmit_') || key.startsWith('node.cpu.')) ? 'stale' : 'valid'
    return { evaluated_at: stamp, sampled_at: data_state === 'no_data' ? null : data_state === 'stale' ? new Date(at - 61000).toISOString() : stamp,
      value: data_state === 'no_data' ? null : data_state === 'stale' ? line.unit === 'percent' ? 99.99 : 999999 : value, data_state }
  })
  body.series.reverse()
}
const reads = state => state.reads.filter(read => read.path.endsWith('/resource_trends'))
const timestamp = (value, locale) => new Intl.DateTimeFormat(locale, { timeZone: 'UTC', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(new Date(value))
const valueText = (value, field, locale) => value === null ? '—' : `${new Intl.NumberFormat(locale, { minimumFractionDigits: field.precision, maximumFractionDigits: field.precision }).format(value)} ${field.unit}`
async function assertTooltip(page, props, index, locale, hidden = []) {
  await page.mouse.move(0, 0)
  await hoverResourceChartPoint(page, index)
  const tooltip = resourceChartTooltip(page)
  await expect(tooltip).toBeVisible()
  await expect(tooltip).toContainText(timestamp(props.rows[index].evaluated_at, locale))
  for (const field of props.config.field_presentations.slice(1)) {
    if (hidden.includes(field.label)) { await expect(tooltip).not.toContainText(field.label); continue }
    await expect(tooltip).toContainText(field.label)
    await expect(tooltip).toContainText(valueText(props.rows[index][field.field], field, locale))
  }
}

const interactionCases = [
  ['cpu', 'node.cpu.busy_percent', {}, ['%'], [8.25]],
  ['cores', 'node.cpu.logical_cores', {}, ['cores'], [18]],
  ['disk-busy', 'node.disk.io_busy_percent', { device: 'sda' }, ['%'], [8.25]],
  ['disk-read-duration', 'node.disk.read_mean_duration_milliseconds', { device: 'sda' }, ['ms'], [2.25]],
  ['disk-write-duration', 'node.disk.write_mean_duration_milliseconds', { device: 'sda' }, ['ms'], [2.25]],
  ['memory', 'node.memory.used_percent', {}, ['GiB', 'GiB', '%'], [64, 34, 8.25]],
  ['disk', 'node.disk.read_bytes_per_second', { device: 'sda' }, ['MiB/s', 'MiB/s'], [2, 4]],
  ['network', 'node.network.receive_bytes_per_second', { device: 'eth0' }, ['MiB/s', 'MiB/s'], [2, 4]],
  ...['1m', '5m', '15m'].map(period => [`load-${period}`, `node.load.average_${period}`, {}, ['load'], [2.25]]),
  ['uptime', 'node.uptime_seconds', {}, ['duration'], [3720 / 3600]],
  ...['total', 'used', 'available', 'free'].map(kind => [`filesystem-${kind}`, `node.filesystem.${kind}_bytes`, mount, ['MiB'], [2]]),
  ['filesystem-percent', 'node.filesystem.used_percent', mount, ['%'], [8.25]],
  ...['total', 'free', 'used'].map(kind => [`inode-${kind}`, `node.filesystem.inodes_${kind}`, mount, ['inodes'], [1008]]),
  ['inode-percent', 'node.filesystem.inodes_used_percent', mount, ['%'], [8.25]],
]
for (const locale of ['zh-cn', 'en']) for (const [family, key, dimensions, units, sample] of interactionCases) test(`${family} legend and hover retain exact values, gaps and scope (${locale})`, async ({ page }) => {
  const state = await resourceBackend(page, { locale, trendResponse: interactionTrend })
  await page.setViewportSize({ width: 620, height: 900 })
  await page.goto(`/node-resources/${node}?metric=${key}&range=5m&refresh=off&${new URLSearchParams(dimensions)}`)
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  await expect.poll(async () => (await resourceChartState(page)).finished).toBe(true)
  const props = await resourceChartProps(page), url = page.url(), requestCount = reads(state).length
  expect(props.rows).toHaveLength(21)
  expect(props.rows[8].evaluated_at).toBe('2026-10-06T23:57:00.000Z')
  expect(props.config.measures.map(measure => props.rows[8][measure])).toEqual(sample)
  expect(props.config.field_presentations.slice(1).map(field => field.unit)).toEqual(units.map(unit => unit === 'load' ? locale === 'en' ? 'tasks' : '个任务' : unit === 'duration' ? locale === 'en' ? 'hours' : '小时' : unit === 'inodes' ? locale === 'en' ? 'inodes' : '个' : unit === 'cores' ? locale === 'en' ? 'cores' : '核' : unit))
  expect(reads(state).at(-1).query).toMatchObject(dimensions)
  if (props.config.measures.length === 1) expect(reads(state).at(-1).query.metrics).toBe(key)
  const measure = props.config.measures[0]
  if (!['memory', 'disk', 'network'].includes(family)) {
    expect(props.rows[0][measure]).toBe(0)
    expect(props.rows[1][measure]).toBeNull()
    expect(props.rows[2][measure]).toBeNull()
  }
  const names = props.config.field_presentations.slice(1).map(field => field.label), hidden = names.at(-1)
  await page.getByText(locale === 'en' ? 'View sample details' : '查看采样明细', { exact: true }).click()
  const evidence = await page.getByTestId('resource-evidence').innerText()
  await assertTooltip(page, props, 8, locale)
  await assertTooltip(page, props, 0, locale)
  await assertTooltip(page, props, 1, locale)
  await assertTooltip(page, props, 2, locale)
  await expect(resourceChartTooltip(page)).not.toContainText('999,999')
  await page.mouse.move(0, 0)
  await expect(resourceChartTooltip(page)).not.toBeVisible()
  await clickResourceChartLegend(page, hidden)
  await expect.poll(async () => (await resourceChartState(page)).active).toEqual(names.filter(name => name !== hidden))
  if (names.length > 1) await assertTooltip(page, props, 8, locale, [hidden])
  else {
    await hoverResourceChartPoint(page, 8)
    await expect(resourceChartTooltip(page)).not.toBeVisible()
  }
  for (const name of names.filter(name => name !== hidden)) await clickResourceChartLegend(page, name)
  expect((await resourceChartState(page)).active).toEqual([])
  await hoverResourceChartPoint(page, 8)
  await expect(resourceChartTooltip(page)).not.toBeVisible()
  for (const name of names) await clickResourceChartLegend(page, name)
  expect((await resourceChartState(page)).active).toEqual(names)
  await dragResourceChartZoom(page, 0.6)
  await expect.poll(async () => (await resourceChartState(page)).start).toBeGreaterThan(50)
  const zoom = (await resourceChartState(page)).start
  await assertTooltip(page, props, 18, locale)
  if (names.length > 1) {
    await clickResourceChartLegend(page, hidden)
    await assertTooltip(page, props, 18, locale, [hidden])
    expect((await resourceChartState(page)).start).toBeCloseTo(zoom, 2)
    await clickResourceChartLegend(page, hidden)
    await assertTooltip(page, props, 18, locale)
  }
  expect((await resourceChartProps(page)).rows).toEqual(props.rows)
  expect(reads(state)).toHaveLength(requestCount)
  expect(page.url()).toBe(url)
  expect(await page.getByTestId('resource-evidence').innerText()).toBe(evidence)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await expect.poll(async () => (await resourceChartState(page)).finished).toBe(true)
  const screenshot = join(mkdtempSync(join(tmpdir(), 'addp-trend-interaction-')), `${family}-${locale}.png`)
  await page.getByTestId('resource-chart').screenshot({ path: screenshot, animations: 'disabled' })
  console.log(`Trend interaction screenshot: ${screenshot}`)
})

for (const key of ['node.cpu.busy_percent', 'node.memory.used_percent']) test(`legend and hover do not change complete period comparison (${key})`, async ({ page }) => {
  const state = await resourceBackend(page, { trendResponse: (body, params) => interactionTrend(body, params, true) })
  await page.goto(`/node-resources/${node}?metric=${key}&range=5m&compare=previous&refresh=off`)
  await expect(page.getByTestId('resource-compare-change')).toHaveText('较上期上升 10.00 个百分点')
  const props = await resourceChartProps(page), target = props.config.field_presentations.at(-1).label
  const comparison = await page.getByTestId('resource-comparison').innerText(), count = reads(state).length
  await clickResourceChartLegend(page, target)
  expect((await resourceChartState(page)).active).not.toContain(target)
  expect(await page.getByTestId('resource-comparison').innerText()).toBe(comparison)
  await clickResourceChartLegend(page, target)
  await dragResourceChartZoom(page, 0.6)
  await expect.poll(async () => (await resourceChartState(page)).start).toBeGreaterThan(50)
  await assertTooltip(page, props, 18, 'zh-cn')
  expect((await resourceChartProps(page)).rows).toEqual(props.rows)
  expect(await page.getByTestId('resource-comparison').innerText()).toBe(comparison)
  expect(reads(state)).toHaveLength(count)
})

for (const [key, dimensions] of [['node.memory.used_percent', {}], ['node.load.average_5m', {}], ['node.filesystem.available_bytes', mount], ['node.filesystem.inodes_used_percent', mount]]) test(`query failure and identity revocation remove visible tooltip and old series (${key})`, async ({ page }) => {
  const state = await resourceBackend(page, { trendResponse: interactionTrend })
  await page.goto(`/node-resources/${node}?metric=${key}&range=5m&refresh=off&${new URLSearchParams(dimensions)}`)
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  const props = await resourceChartProps(page)
  await assertTooltip(page, props, 8, 'zh-cn')
  state.trendMode = 'budget'
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('resource-trend-error')).toBeVisible()
  await expect(page.getByTestId('resource-chart')).toHaveCount(0)
  await expect(resourceChartTooltip(page)).toHaveCount(0)
  state.trendMode = ''
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  await assertTooltip(page, props, 8, 'zh-cn')
  state.identity = identity([])
  await setIdentity(page, state.identity)
  await expect(page.getByTestId('resource-chart')).toHaveCount(0)
  await expect(resourceChartTooltip(page)).toHaveCount(0)
})
