import { expect, test } from '@playwright/test'
import { backend, permissions } from './process-resources.fixture'
import { identity, setIdentity } from './monitoring-targets.fixture'
import { trackResourceChart, resourceChartProps, resourceChartState, dragResourceChartZoom,
  clickResourceChartLegend, hoverResourceChartPoint, resourceChartTooltip } from './resource-chart.fixture'

test.beforeEach(({ page }) => trackResourceChart(page))
const instances = [1, 2].map(id => ({ id, process_started_at: '2026-10-09T23:00:00Z' }))
const chartFor = (page, metric) => page.getByTestId(`process-trend-${metric}`).locator('.chart-renderer')
const chartState = (page, chart) => resourceChartState(page, null, chart)
async function readyChart(page, metric) {
  const chart = chartFor(page, metric)
  await expect(chart.locator('canvas')).toBeVisible()
  await expect.poll(async () => (await chartState(page, chart)).finished).toBe(true)
  return chart
}
const refresh = (page, locale = 'zh-cn') => page.getByTestId('process-trend').getByRole('button', { name: locale === 'en' ? 'Refresh' : '刷新', exact: true }).click()

for (const locale of ['zh-cn', 'en']) test(`process CPU and RSS zoom independently without changing samples or queries (${locale})`, async ({ page }, info) => {
  const state = await backend(page, { locale, instances, trendResponse: body => {
    for (const line of body.series) {
      line.points[0].value = 0
      Object.assign(line.points[3], { data_state: 'stale', value: 999999, sampled_at: new Date(Date.parse(line.points[3].evaluated_at) - 61000).toISOString() })
    }
  } })
  await page.setViewportSize({ width: 620, height: 900 })
  await page.goto('/service-resources?instance_id=1&refresh=off')
  const cpu = await readyChart(page, 'cpu'), memory = await readyChart(page, 'memory')
  await expect(page.getByTestId('process-zoom-hint')).toHaveText(Array(2).fill(locale === 'en' ? 'Drag the slider below the chart to focus on a time range.' : '拖动图下滑块，查看局部时间范围。'))
  const initial = [await resourceChartProps(page, cpu), await resourceChartProps(page, memory)]
  for (const props of initial) {
    expect(props.zoomable).toBe(true)
    expect(props.rows).toHaveLength(21)
    expect(props.rows[0].value).toBe(0)
    expect(props.rows[2].value).toBeNull()
    expect(props.rows[3].value).toBeNull()
  }
  await hoverResourceChartPoint(page, 0, cpu)
  await expect(resourceChartTooltip(page, cpu)).toContainText(`0.00 ${initial[0].config.field_presentations[1].unit}`)
  const requestCount = state.trendCalls.length, url = page.url()
  await dragResourceChartZoom(page, 0.6, cpu)
  const cpuStart = (await chartState(page, cpu)).start
  expect(cpuStart).toBeGreaterThan(50)
  expect((await chartState(page, memory)).start).toBe(0)
  await dragResourceChartZoom(page, 0.4, memory)
  const memoryStart = (await chartState(page, memory)).start
  expect(memoryStart).toBeGreaterThan(30)
  expect((await chartState(page, cpu)).start).toBeCloseTo(cpuStart, 2)
  for (const [index, chart] of [cpu, memory].entries()) {
    expect((await resourceChartProps(page, chart)).rows).toEqual(initial[index].rows)
    expect((await chartState(page, chart)).lengths).toEqual([21])
  }
  await hoverResourceChartPoint(page, 18, memory)
  await expect(resourceChartTooltip(page, memory)).toContainText('1.00 GiB')
  const legend = initial[0].config.field_presentations[1].label
  await clickResourceChartLegend(page, legend, cpu)
  await expect.poll(async () => (await chartState(page, cpu)).active).toEqual([])
  await expect(resourceChartTooltip(page, cpu)).not.toBeVisible()
  await clickResourceChartLegend(page, legend, cpu)
  await expect.poll(async () => (await chartState(page, cpu)).active).toEqual([legend])
  expect(state.trendCalls).toHaveLength(requestCount)
  expect(page.url()).toBe(url)
  await page.evaluate(() => document.documentElement.style.setProperty('--addp-border-color', 'rgb(30, 40, 50)'))
  await expect.poll(async () => (await chartState(page, cpu)).borderColor).toBe('rgb(30, 40, 50)')
  expect((await chartState(page, cpu)).start).toBeCloseTo(cpuStart, 2)
  state.setEnd('2026-10-10T00:00:15Z')
  await refresh(page, locale)
  await readyChart(page, 'cpu'); await readyChart(page, 'memory')
  expect((await chartState(page, cpu)).start).toBeCloseTo(cpuStart, 2)
  expect((await chartState(page, memory)).start).toBeCloseTo(memoryStart, 2)
  expect(state.trendCalls.at(-1)).toMatchObject({ start: '2026-10-09T23:55:15Z', end: '2026-10-10T00:00:15Z' })
  await page.getByTestId('process-trend').locator('.el-select').first().click()
  await page.getByRole('option', { name: locale === 'en' ? 'Last hour' : '近 1 小时', exact: true }).click()
  await readyChart(page, 'cpu'); await readyChart(page, 'memory')
  expect((await chartState(page, cpu)).start).toBe(0)
  expect((await chartState(page, memory)).start).toBe(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: info.outputPath(`process-zoom-${locale}.png`), fullPage: true, animations: 'disabled' })
})

test('automatic process refresh retains both viewports and closing or switching instance resets them', async ({ page }) => {
  const state = await backend(page, { instances })
  await page.clock.install()
  await page.goto('/service-resources?instance_id=1&refresh=60')
  const cpu = await readyChart(page, 'cpu'), memory = await readyChart(page, 'memory')
  await dragResourceChartZoom(page, 0.6, cpu)
  await dragResourceChartZoom(page, 0.4, memory)
  const starts = [(await chartState(page, cpu)).start, (await chartState(page, memory)).start]
  const count = state.trendCalls.length
  state.setEnd('2026-10-10T00:00:15Z')
  await page.clock.fastForward(60001)
  await expect.poll(() => state.trendCalls.length).toBe(count + 1)
  await readyChart(page, 'cpu'); await readyChart(page, 'memory')
  expect((await chartState(page, cpu)).start).toBeCloseTo(starts[0], 2)
  expect((await chartState(page, memory)).start).toBeCloseTo(starts[1], 2)
  await page.locator('.el-dialog__headerbtn').click()
  await expect(page).toHaveURL(url => !url.searchParams.has('instance_id'))
  await page.getByTestId('process-list').getByRole('button', { name: '查看趋势', exact: true }).first().click()
  await readyChart(page, 'cpu')
  expect((await chartState(page, cpu)).start).toBe(0)
  await dragResourceChartZoom(page, 0.6, cpu)
  await page.locator('.el-dialog__headerbtn').click()
  await page.getByTestId('process-list').getByRole('button', { name: '查看趋势', exact: true }).nth(1).click()
  await readyChart(page, 'cpu'); await readyChart(page, 'memory')
  expect(state.trendCalls.at(-1).instance_id).toBe('2')
  expect((await chartState(page, cpu)).start).toBe(0)
  expect((await chartState(page, memory)).start).toBe(0)
})

for (const failure of ['unavailable', 'denied', 'wrong-owner']) test(`process ${failure} clears charts and viewports before recovery`, async ({ page }) => {
  const state = await backend(page, { instances })
  await page.goto('/service-resources?instance_id=1&refresh=off')
  const cpu = await readyChart(page, 'cpu')
  await dragResourceChartZoom(page, 0.6, cpu)
  state.setTrendMode(failure)
  await refresh(page)
  await expect(page.getByTestId('process-trend-error')).toBeVisible()
  await expect(page.getByTestId('process-trend').locator('.chart-renderer')).toHaveCount(0)
  state.setTrendMode('valid')
  await refresh(page)
  await readyChart(page, 'cpu'); await readyChart(page, 'memory')
  expect((await chartState(page, cpu)).start).toBe(0)
  expect((await chartState(page, chartFor(page, 'memory'))).start).toBe(0)
})

test('identity changes clear process viewports and revocation removes the old charts', async ({ page }) => {
  await backend(page, { instances })
  await page.goto('/service-resources?instance_id=1&refresh=off')
  const cpu = await readyChart(page, 'cpu')
  await dragResourceChartZoom(page, 0.6, cpu)
  await setIdentity(page, { ...identity(permissions), principal: { type: 'user', id: 'next-fixture-user' } })
  await readyChart(page, 'cpu')
  await expect.poll(async () => (await chartState(page, cpu)).start).toBe(0)
  await dragResourceChartZoom(page, 0.6, cpu)
  await setIdentity(page, identity([]))
  await expect(page.getByTestId('process-trend').locator('.chart-renderer')).toHaveCount(0)
  await setIdentity(page, identity(permissions))
  await readyChart(page, 'cpu')
  expect((await chartState(page, cpu)).start).toBe(0)
})
