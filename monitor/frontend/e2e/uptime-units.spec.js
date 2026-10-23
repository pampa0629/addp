import { expect, test } from '@playwright/test'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { node, resourceBackend } from './node-resources.fixture'
import { trackResourceChart, resourceChartProps, resourceChartState, dragResourceChartZoom,
  hoverResourceChartPoint, resourceChartTooltip, clickResourceChartLegend } from './resource-chart.fixture'

test.beforeEach(({ page }) => trackResourceChart(page))

for (const locale of ['zh-cn', 'en']) test(`uptime axis and hover use one duration scale while evidence retains elapsed time (${locale})`, async ({ page }) => {
  let seconds = 30, missing = false
  const state = await resourceBackend(page, { locale, trendResponse(body, params) {
    const start = Date.parse(params.get('start')), end = Date.parse(params.get('end'))
    body.step_seconds = 15
    body.series[0].points = Array.from({ length: (end - start) / 15000 + 1 }, (_, index) => {
      const stamp = new Date(start + index * 15000).toISOString()
      const data_state = missing || index === 1 ? 'no_data' : index === 2 ? 'stale' : 'valid'
      return { evaluated_at: stamp, sampled_at: data_state === 'no_data' ? null : data_state === 'stale' ? new Date(Date.parse(stamp) - 61000).toISOString() : stamp,
        value: data_state === 'no_data' ? null : data_state === 'stale' ? 999999999 : index === 0 ? 0 : seconds, data_state }
    })
  } })
  await page.setViewportSize({ width: 620, height: 900 })
  await page.goto(`/node-resources/${node}?metric=node.uptime_seconds&range=5m&refresh=off`)
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  const refresh = () => page.getByRole('button', { name: locale === 'en' ? 'Refresh' : '刷新', exact: true }).click()
  let zoom = 0
  for (const [raw, divisor, zhUnit, enUnit, zhElapsed, enElapsed, display] of [
    [30, 1, '秒', 'seconds', '30秒钟', '30 seconds', '30.00'],
    [90, 60, '分钟', 'minutes', '1分钟 30秒钟', '1 minute 30 seconds', '1.50'],
    [5400, 3600, '小时', 'hours', '1小时 30分钟', '1 hour 30 minutes', '1.50'],
    [286435, 86400, '天', 'days', '3天 7小时 33分钟', '3 days 7 hours 33 minutes', '3.32'],
  ]) {
    if (seconds !== raw) { seconds = raw; await refresh() }
    const unit = locale === 'en' ? enUnit : zhUnit
    await expect.poll(async () => (await resourceChartProps(page)).config.field_presentations.at(-1).unit).toBe(unit)
    const props = await resourceChartProps(page)
    expect(props.rows).toHaveLength(21)
    expect(props.rows[0].uptime).toBe(0)
    expect(props.rows[1].uptime).toBeNull()
    expect(props.rows[2].uptime).toBeNull()
    expect(props.rows.at(-1).uptime).toBe(raw / divisor)
    expect(props.config.field_presentations.at(-1).precision).toBe(2)
    expect((await resourceChartState(page)).start).toBeCloseTo(zoom, 2)
    await page.getByText(locale === 'en' ? 'View sample details' : '查看采样明细', { exact: true }).click()
    await page.getByTestId('resource-evidence').scrollIntoViewIfNeeded()
    await expect(page.getByTestId('resource-evidence')).toContainText(locale === 'en' ? enElapsed : zhElapsed)
    await expect(page.getByTestId('resource-uptime').locator('strong')).toHaveText(locale === 'en' ? '1 hour' : '1小时')
    const count = state.reads.length, url = page.url()
    if (!zoom) {
      await dragResourceChartZoom(page, 0.6)
      await expect.poll(async () => (await resourceChartState(page)).start).toBeGreaterThan(50)
      zoom = (await resourceChartState(page)).start
    }
    await hoverResourceChartPoint(page, 18)
    await expect(resourceChartTooltip(page)).toBeVisible()
    await expect(resourceChartTooltip(page)).toContainText(`${display} ${unit}`)
    expect((await resourceChartProps(page)).rows).toEqual(props.rows)
    expect(state.reads).toHaveLength(count)
    expect(page.url()).toBe(url)
  }
  const label = locale === 'en' ? 'Uptime' : '运行时长'
  await clickResourceChartLegend(page, label)
  expect((await resourceChartState(page)).active).toEqual([])
  await hoverResourceChartPoint(page, 18)
  await expect(resourceChartTooltip(page)).not.toBeVisible()
  await clickResourceChartLegend(page, label)
  await hoverResourceChartPoint(page, 18)
  await expect(resourceChartTooltip(page)).toContainText(locale === 'en' ? '3.32 days' : '3.32 天')
  await expect.poll(async () => (await resourceChartState(page)).finished).toBe(true)
  const screenshot = join(mkdtempSync(join(tmpdir(), 'addp-uptime-units-')), `${locale}.png`)
  await page.getByTestId('resource-chart').screenshot({ path: screenshot, animations: 'disabled' })
  console.log(`Uptime units screenshot: ${screenshot}`)
  missing = true
  await refresh()
  await expect.poll(async () => (await resourceChartProps(page)).rows.every(row => row.uptime === null)).toBe(true)
  expect((await resourceChartProps(page)).config.field_presentations.at(-1).unit).toBe(locale === 'en' ? 'seconds' : '秒')
  expect((await resourceChartState(page)).start).toBeCloseTo(zoom, 2)
  await hoverResourceChartPoint(page, 18)
  await expect(resourceChartTooltip(page)).toContainText('—')
  state.trendMode = 'budget'
  await refresh()
  await expect(page.getByTestId('resource-trend-error')).toBeVisible()
  await expect(page.getByTestId('resource-chart')).toHaveCount(0)
  missing = false; seconds = 0; state.trendMode = ''
  await refresh()
  await expect.poll(async () => (await resourceChartProps(page)).rows.at(-1).uptime).toBe(0)
  expect((await resourceChartState(page)).start).toBe(0)
  expect((await resourceChartProps(page)).config.field_presentations.at(-1).unit).toBe(locale === 'en' ? 'seconds' : '秒')
  await hoverResourceChartPoint(page, 18)
  await expect(resourceChartTooltip(page)).toContainText(locale === 'en' ? '0.00 seconds' : '0.00 秒')
  expect(state.reads.filter(read => read.path.endsWith('/resource_trends')).every(read => read.query.metrics === 'node.uptime_seconds')).toBe(true)
})
