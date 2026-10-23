import { expect, test } from '@playwright/test'
import { backend, permissions } from './process-resources.fixture'
import { identity, setIdentity } from './monitoring-targets.fixture'
import { trackResourceChart, resourceChartProps, resourceChartState, dragResourceChartZoom } from './resource-chart.fixture'
const url = '/service-resources?instance_id=1&compare=previous&refresh=off'
const instances = [1, 2].map(id => ({ id, process_started_at: '2026-10-07T00:00:00Z' }))
function complete(body, params, cpu = [0.125, 1.25], rss = [2 * 1024 ** 3, 1024 ** 3]) {
  const boundary = (Date.parse(params.start) + Date.parse(params.end)) / 2
  for (const line of body.series) for (const point of line.points) {
    Object.assign(point, { data_state: 'valid', sampled_at: point.evaluated_at, value: (line.unit === 'cores' ? cpu : rss)[Date.parse(point.evaluated_at) <= boundary ? 0 : 1] })
  }
}
const refresh = (page, locale = 'zh-cn') => page.getByTestId('process-trend').getByRole('button', { name: locale === 'en' ? 'Refresh' : '刷新', exact: true }).click()
const chartFor = (page, metric) => page.getByTestId(`process-trend-${metric}`).locator('.chart-renderer')
for (const locale of ['zh-cn', 'en']) test(`process comparison shows local means and original-unit changes beside both charts (${locale})`, async ({ page }, info) => {
  await trackResourceChart(page)
  const state = await backend(page, { locale, instances, trendResponse: complete })
  await page.setViewportSize({ width: 620, height: 900 })
  await page.goto(url)
  const cpu = page.getByTestId('process-comparison-cpu'), memory = page.getByTestId('process-comparison-memory')
  await expect(page.getByTestId('process-compare-cpu-current').locator('strong')).toHaveText(locale === 'en' ? '1.25 cores' : '1.25 核')
  await expect(page.getByTestId('process-compare-cpu-previous').locator('strong')).toHaveText(locale === 'en' ? '0.13 cores' : '0.13 核')
  await expect(page.getByTestId('process-compare-cpu-change')).toHaveText(locale === 'en' ? 'Up 1.13 cores from the previous period' : '较上期增加 1.13 核')
  await expect(page.getByTestId('process-compare-memory-change')).toHaveText(locale === 'en' ? 'Down 1.00 GiB from the previous period' : '较上期减少 1.00 GiB')
  await expect(page.getByTestId('process-compare-memory-current').locator('strong')).toHaveText('1.00 GiB')
  await expect(cpu).toContainText(locale === 'en' ? 'Valid evaluation points: 20 / 20' : '有效评估点：20 / 20')
  await expect(cpu).toContainText(locale === 'en' ? 'not a percentage' : '不是百分比')
  expect(state.trendCalls).toEqual([{ instance_id: '1', start: '2026-10-09T23:50:00Z', end: '2026-10-10T00:00:00Z' }])
  expect(state.listCalls[0]).not.toHaveProperty('compare')
  for (const metric of ['cpu', 'memory']) {
    const chart = chartFor(page, metric)
    await expect(chart.locator('canvas')).toBeVisible()
    expect((await resourceChartProps(page, chart)).rows).toHaveLength(20)
  }
  const chart = chartFor(page, 'cpu')
  await expect.poll(async () => (await resourceChartState(page, null, chart)).finished).toBe(true)
  await dragResourceChartZoom(page, 0.4, chart)
  await expect(page.getByTestId('process-compare-cpu-change')).toContainText('1.13')
  expect(state.trendCalls).toHaveLength(1)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await cpu.screenshot({ path: info.outputPath(`cpu-comparison-${locale}.png`) })
  await memory.screenshot({ path: info.outputPath(`memory-comparison-${locale}.png`) })
})
test('opt-in, refresh, reload, opt-out and close keep one canonical query without leaking comparison into System', async ({ page }) => {
  const state = await backend(page, { instances, trendResponse: complete })
  await page.goto('/service-resources?instance_id=1&refresh=off')
  await expect(chartFor(page, 'cpu').locator('canvas')).toBeVisible()
  await page.getByTestId('process-compare').check()
  await expect(page.getByTestId('process-compare-cpu-change')).toBeVisible()
  await expect(page).toHaveURL(/compare=previous/)
  expect(state.trendCalls).toHaveLength(2)
  state.setEnd('2026-10-10T00:00:15Z')
  await refresh(page)
  await expect(page.getByTestId('process-compare-cpu-change')).toBeVisible()
  expect(state.trendCalls.at(-1).start).toBe('2026-10-09T23:50:15Z')
  await page.reload()
  await expect(page.getByTestId('process-compare-cpu-change')).toBeVisible()
  expect(state.trendCalls).toHaveLength(4)
  let releaseOptOut, waitingForSummary = false, blockSummary = true
  const optOutSummary = new Promise(resolve => { releaseOptOut = resolve })
  await page.route('**/api/v1/monitor/platform/process_resource_summaries*', async route => {
    if (blockSummary) { waitingForSummary = true; await optOutSummary }
    await route.fallback()
  })
  await page.getByTestId('process-compare').uncheck()
  await expect(page.getByTestId('process-comparison-cpu')).toHaveCount(0)
  await expect(page).not.toHaveURL(/compare=/)
  // The route clears comparison before fresh summaries can anchor the new query.
  await expect.poll(() => waitingForSummary).toBe(true)
  expect(state.trendCalls).toHaveLength(4)
  expect(state.trendCalls.at(-1).start).toBe('2026-10-09T23:50:15Z')
  blockSummary = false; releaseOptOut()
  await expect.poll(() => state.trendCalls.length).toBe(5)
  expect(state.trendCalls.at(-1).start).toBe('2026-10-09T23:55:15Z')
  await page.getByTestId('process-compare').check()
  await expect(page.getByTestId('process-compare-cpu-change')).toBeVisible()
  await page.locator('.el-dialog__headerbtn').click()
  await expect(page).not.toHaveURL(/compare=|instance_id=/)
  expect(state.listCalls.every(params => !('compare' in params))).toBe(true)
})
for (const [range, label, start, count] of [['1h', '近 1 小时', '2026-10-09T22:00:00Z', 240], ['24h', '近 24 小时', '2026-10-08T00:00:00Z', 480]]) test(`${range} uses a bounded doubled window and retains comparison when changing supported range`, async ({ page }) => {
  const state = await backend(page, { instances, trendResponse: complete })
  await page.goto(url)
  await expect(page.getByTestId('process-compare-cpu-change')).toBeVisible()
  await page.getByTestId('process-trend').locator('.el-select').first().click()
  await page.getByRole('option', { name: label, exact: true }).click()
  await expect(page.getByTestId('process-compare-cpu-current')).toContainText(`${count} / ${count}`)
  await expect(page).toHaveURL(/compare=previous/)
  expect(state.trendCalls.at(-1).start).toBe(start)
})
test('7-day trend removes comparison and never requests fourteen days', async ({ page }) => {
  const state = await backend(page, { instances, trendResponse: complete })
  await page.goto('/service-resources?instance_id=1&range=7d&compare=previous&refresh=off')
  await expect(page).not.toHaveURL(/compare=/)
  await expect(chartFor(page, 'cpu').locator('canvas')).toBeVisible()
  await expect(page.getByTestId('process-compare')).toBeDisabled()
  await expect(page.getByTestId('process-compare-retention')).toContainText('14 天')
  expect(state.trendCalls).toHaveLength(1)
  expect(state.trendCalls[0].start).toBe('2026-10-03T00:00:00Z')
})
for (const missing of ['no_data', 'stale', 'not_connected']) test(`${missing} in either period suppresses that metric only`, async ({ page }) => {
  let index = 1
  await backend(page, { instances, trendResponse: (body, params) => {
    complete(body, params)
    const p = body.series[0].points[index]
    Object.assign(p, { data_state: missing, sampled_at: missing === 'stale' ? new Date(Date.parse(p.evaluated_at) - 61000).toISOString() : null, value: missing === 'stale' ? p.value : null })
  } })
  await page.goto(url)
  for (const i of [1, 21]) {
    if (i !== index) { index = i; await refresh(page) }
    await expect(page.getByTestId('process-compare-cpu-incomplete')).toContainText('样本不足')
    await expect(page.getByTestId('process-compare-cpu-change')).toHaveCount(0)
    await expect(page.getByTestId('process-compare-cpu-previous').locator('strong')).toHaveText('—')
    await expect(page.getByTestId('process-compare-memory-change')).toBeVisible()
    await expect(page.getByTestId(`process-compare-cpu-${i === 1 ? 'previous' : 'current'}`)).toContainText('19 / 20')
  }
})
test('launch-clipped history shows expected missing coverage and no partial-window conclusion', async ({ page }) => {
  await backend(page, { trendResponse: complete })
  await page.goto(url)
  for (const metric of ['cpu', 'memory']) {
    await expect(page.getByTestId(`process-compare-${metric}-incomplete`)).toContainText('样本不足')
    await expect(page.getByTestId(`process-compare-${metric}-previous`)).toContainText('0 / 20')
    await expect(page.getByTestId(`process-comparison-${metric}`)).toContainText('本次进程启动后')
  }
  await expect(chartFor(page, 'cpu').locator('canvas')).toBeVisible()
})
for (const failure of ['wrong-owner', 'unavailable', 'denied']) test(`${failure} clears old comparison and restores it only after valid re-read`, async ({ page }) => {
  const state = await backend(page, { instances, trendResponse: complete })
  await page.goto(url)
  await expect(page.getByTestId('process-compare-cpu-change')).toBeVisible()
  state.setTrendMode(failure)
  await refresh(page)
  await expect(page.getByTestId('process-trend-error')).toBeVisible()
  await expect(page.getByTestId('process-comparison-cpu')).toHaveCount(0)
  await expect(chartFor(page, 'memory')).toHaveCount(0)
  state.setTrendMode('valid')
  await refresh(page)
  await expect(page.getByTestId('process-compare-cpu-change')).toBeVisible()
})
test('zero baseline, equal values and small CPU changes use original units without percentage growth', async ({ page }) => {
  let cpu = [0, 1], rss = [0, 0]
  await backend(page, { instances, trendResponse: (body, params) => complete(body, params, cpu, rss) })
  await page.goto(url)
  await expect(page.getByTestId('process-compare-cpu-change')).toHaveText('较上期增加 1.00 核')
  await expect(page.getByTestId('process-compare-memory-change')).toHaveText('与上期持平')
  cpu = [1.005, 1.003]; rss = [1024, 2048]
  await refresh(page)
  await expect(page.getByTestId('process-compare-cpu-change')).toHaveText('较上期变化不足 0.01 核')
  await expect(page.getByTestId('process-compare-memory-change')).toHaveText('较上期增加 1.00 KiB')
})
test('revoked identity rejects a held trend without repopulating comparison', async ({ page }) => {
  const state = await backend(page, { instances, trendResponse: complete, trendHeld: true })
  await page.goto(url)
  await expect.poll(() => state.trendCalls.length).toBe(1)
  await setIdentity(page, identity(permissions, { type: 'tenant', tenant_id: '8' }))
  state.release()
  await expect(page.getByTestId('process-comparison-cpu')).toHaveCount(0)
  await expect(page.getByTestId('process-trend')).toHaveCount(0)
  await setIdentity(page, identity(permissions))
  await expect(page.getByTestId('process-compare-cpu-change')).toBeVisible()
})
