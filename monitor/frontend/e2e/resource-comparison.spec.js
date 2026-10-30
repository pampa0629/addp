import { expect, test } from '@playwright/test'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { identity, node, resourceBackend, resourcePermissions, setIdentity, completeComparisonTrend as completeTrend } from './node-resources.fixture'


const reads = state => state.reads.filter(row => row.path.endsWith('/resource_trends'))
const comparisonURL = `/node-resources/${node}?range=5m&compare=previous&refresh=off`

for (const locale of ['zh-cn', 'en']) test(`comparison shows local period means and percentage points from one bounded response (${locale})`, async ({ page }) => {
  const state = await resourceBackend(page, { locale, trendResponse: completeTrend })
  await page.setViewportSize({ width: 620, height: 900 })
  await page.goto(comparisonURL)
  const panel = page.getByTestId('resource-comparison')
  await expect(page.getByTestId('resource-compare-current').locator('strong')).toHaveText('40.00 %')
  await expect(page.getByTestId('resource-compare-previous').locator('strong')).toHaveText('30.00 %')
  await expect(page.getByTestId('resource-compare-change')).toHaveText(locale === 'en' ? 'Up 10.00 percentage points from the previous period' : '较上期上升 10.00 个百分点')
  await expect(panel).toContainText(locale === 'en' ? 'Valid evaluation points: 20 / 20' : '有效评估点：20 / 20')
  await expect(panel).toContainText(locale === 'en' ? 'evaluated every 15 seconds' : '每 15 秒评估一次')
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  expect(reads(state)).toHaveLength(1)
  expect(reads(state)[0].query).toEqual({ node_id: node, metrics: 'node.memory.total_bytes,node.memory.available_bytes,node.memory.used_percent', start: '2026-10-06T23:50:00.000Z', end: '2026-10-07T00:00:00.000Z' })
  await page.getByText(locale === 'en' ? 'View sample details' : '查看采样明细', { exact: true }).click()
  await expect(page.getByTestId('resource-evidence')).toContainText('40.00 %')
  await expect(page.getByTestId('resource-evidence')).not.toContainText('30.00 %')
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await panel.scrollIntoViewIfNeeded()
  await expect(panel.getByRole('heading').first()).toBeInViewport()
  await panel.screenshot({ path: join(tmpdir(), `addp-resource-comparison-${locale}.png`), animations: 'disabled' })
})

test('comparison opt-in, refresh and opt-out use one query each and survive reload', async ({ page }) => {
  const state = await resourceBackend(page, { trendResponse: completeTrend })
  await page.goto(`/node-resources/${node}?range=5m&refresh=off`)
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  expect(reads(state)).toHaveLength(1)
  expect(reads(state)[0].query.start).toBe('2026-10-06T23:55:00.000Z')
  await page.getByTestId('resource-compare').check()
  await expect(page.getByTestId('resource-compare-change')).toBeVisible()
  expect(reads(state)).toHaveLength(2)
  await expect(page).toHaveURL(/compare=previous/)
  await page.reload()
  await expect(page.getByTestId('resource-compare-current').locator('strong')).toHaveText('40.00 %')
  expect(reads(state)).toHaveLength(3)
  await page.getByTestId('resource-compare').uncheck()
  await expect(page.getByTestId('resource-comparison')).toHaveCount(0)
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  expect(reads(state)).toHaveLength(4)
  expect(reads(state).at(-1).query.start).toBe('2026-10-06T23:55:00.000Z')
  await expect(page).not.toHaveURL(/compare=/)
})

test('daily comparison uses a single 48-hour window and the planned coarser step', async ({ page }) => {
  const state = await resourceBackend(page, { trendResponse: completeTrend })
  await page.goto(`/node-resources/${node}?range=24h&compare=previous&refresh=off`)
  await expect(page.getByTestId('resource-compare-change')).toHaveText('较上期上升 10.00 个百分点')
  await expect(page.getByTestId('resource-compare-current')).toContainText('有效评估点：480 / 480')
  await expect(page.getByTestId('resource-comparison')).toContainText('每 180 秒评估一次')
  expect(reads(state)).toHaveLength(1)
  expect(reads(state)[0].query.start).toBe('2026-10-05T00:00:00.000Z')
})

test('historical comparison keeps the fixed anchor, CPU rolling meaning and distinct current cards', async ({ page }) => {
  const state = await resourceBackend(page, { trendResponse: completeTrend })
  await page.goto(`/node-resources/${node}?range=1h&metric=node.cpu.busy_percent&compare=previous&end=2026-10-06T20:00:00Z`)
  await expect(page.getByTestId('resource-compare-change')).toBeVisible()
  await expect(page).toHaveURL(url => url.searchParams.get('end') === '2026-10-06T20:00:00.000Z' && url.searchParams.get('refresh') === 'off')
  await page.getByTestId('resource-trend-help').getByRole('button').focus()
  await expect(page.getByRole('tooltip')).toContainText('CPU 每个点反映此前 1 分钟的忙碌率')
  await expect(page.getByTestId('resource-cpuBusy').locator('strong')).toHaveText('0.00 %')
  expect(reads(state)[0].query).toMatchObject({ start: '2026-10-06T18:00:00.000Z', end: '2026-10-06T20:00:00.000Z' })
  state.serverEnd = '2026-10-07T01:00:00Z'
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('node-resources')).toHaveAttribute('aria-busy', 'false')
  await expect(page.getByTestId('resource-compare-change')).toBeVisible()
  expect(reads(state).at(-1).query.end).toBe('2026-10-06T20:00:00.000Z')
})

test('unsupported metric and 7-day range remove comparison without requesting 14 days', async ({ page }) => {
  const state = await resourceBackend(page, { trendResponse: completeTrend })
  await page.goto(comparisonURL)
  await expect(page.getByTestId('resource-compare-change')).toBeVisible()
  await page.getByTestId('resource-range').click()
  await page.getByRole('option', { name: '近 7 天', exact: true }).click()
  await expect(page).not.toHaveURL(/compare=/)
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  await expect(page.getByTestId('resource-compare')).toBeDisabled()
  await expect(page.getByTestId('resource-compare-retention')).toContainText('需要约 14 天历史')
  expect(reads(state).at(-1).query.start).toBe('2026-09-30T00:00:00.000Z')
  await page.getByTestId('resource-metric').click()
  await page.getByRole('option', { name: '系统平均负载（1 分钟）', exact: true }).click()
  await expect(page.getByTestId('resource-compare')).toHaveCount(0)
  await expect(page.getByTestId('resource-comparison')).toHaveCount(0)
})

for (const stateName of ['no_data', 'stale', 'not_connected']) test(`${stateName} in either period blocks means and change while preserving current observations`, async ({ page }) => {
  let missingIndex = 1
  await resourceBackend(page, { trendResponse(body, params) {
    completeTrend(body, params)
    const point = body.series.find(line => line.unit === 'percent').points[missingIndex]
    Object.assign(point, { data_state: stateName, value: stateName === 'stale' ? point.value : null, sampled_at: stateName === 'stale' ? new Date(Date.parse(point.evaluated_at) - 61000).toISOString() : null })
  } })
  await page.goto(comparisonURL)
  for (const index of [1, 21]) {
    if (index !== missingIndex) { missingIndex = index; await page.getByRole('button', { name: '刷新', exact: true }).click() }
    await expect(page.getByTestId('resource-compare-incomplete')).toContainText('样本不足，暂不比较')
    await expect(page.getByTestId('resource-compare-change')).toHaveCount(0)
    await expect(page.getByTestId('resource-compare-current').locator('strong')).toHaveText('—')
    await expect(page.getByTestId('resource-compare-previous').locator('strong')).toHaveText('—')
    await expect(page.getByTestId(`resource-compare-${index === 1 ? 'previous' : 'current'}`)).toContainText('19 / 20')
    await expect(page.getByTestId('resource-cpuBusy').locator('strong')).toHaveText('0.00 %')
  }
})

for (const failure of ['budget', 'unavailable', 'denied', 'invalid-grid', 'expired-valid']) test(`${failure} clears the complete comparison and can recover without fallback queries`, async ({ page }) => {
  let invalid = false
  const state = await resourceBackend(page, { trendResponse(body, params) {
    completeTrend(body, params)
    if (invalid && failure === 'invalid-grid') body.series.find(line => line.unit === 'percent').points.pop()
    if (invalid && failure === 'expired-valid') body.series.find(line => line.unit === 'percent').points[5].sampled_at = body.start
  } })
  await page.goto(comparisonURL)
  await expect(page.getByTestId('resource-compare-change')).toBeVisible()
  if (['budget', 'unavailable', 'denied'].includes(failure)) state.trendMode = failure
  else invalid = true
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId(failure === 'denied' ? 'resource-error' : 'resource-trend-error')).toBeVisible()
  await expect(page.getByTestId('resource-comparison')).toHaveCount(0)
  expect(reads(state)).toHaveLength(2)
  if (failure === 'denied') {
    await expect(page.getByTestId('resource-cpuBusy')).toHaveCount(0)
    await expect(page.getByTestId('resource-chart')).toHaveCount(0)
  } else await expect(page.getByTestId('resource-cpuBusy').locator('strong')).toHaveText('0.00 %')
  state.trendMode = ''; invalid = false
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('resource-compare-change')).toBeVisible()
  expect(reads(state)).toHaveLength(3)
})

test('zero baseline, falling usage, equality and tiny changes have readable local wording', async ({ page }) => {
  let previous = 0, current = 40
  await resourceBackend(page, { trendResponse: (body, params) => completeTrend(body, params, previous, current) })
  await page.goto(comparisonURL)
  for (const [before, after, label] of [[0, 40, '较上期上升 40.00 个百分点'], [40, 30, '较上期下降 10.00 个百分点'], [30, 30, '与上期持平'], [30.004, 30.006, '较上期变化不足 0.01 个百分点']]) {
    if (previous !== before || current !== after) { previous = before; current = after; await page.getByRole('button', { name: '刷新', exact: true }).click() }
    await expect(page.getByTestId('resource-compare-change')).toHaveText(label)
  }
})

test('identity revocation clears a visible comparison and rejects the late refreshed response', async ({ page }) => {
  const state = await resourceBackend(page, { trendResponse: completeTrend })
  await page.goto(comparisonURL)
  await expect(page.getByTestId('resource-compare-change')).toBeVisible()
  state.holdTrend()
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect.poll(() => reads(state).length).toBe(2)
  await expect(page.getByTestId('resource-comparison')).toBeVisible()
  await setIdentity(page, identity(resourcePermissions, { type: 'tenant', tenant_id: '8' }))
  await expect(page.getByTestId('resource-comparison')).toHaveCount(0)
  state.releaseTrend()
  await expect(page.getByTestId('node-resources')).toHaveCount(0)
  await setIdentity(page, identity(resourcePermissions))
  await expect(page.getByTestId('resource-compare-change')).toBeVisible()
  expect(reads(state)).toHaveLength(3)
})

test('a delayed comparison cannot repopulate results after switching to a non-comparable metric', async ({ page }) => {
  const state = await resourceBackend(page, { holdTrend: true, trendResponse: completeTrend })
  await page.goto(comparisonURL)
  await expect.poll(() => reads(state).length).toBe(1)
  await page.getByTestId('resource-metric').click()
  await page.getByRole('option', { name: '系统平均负载（1 分钟）', exact: true }).click()
  await expect.poll(() => reads(state).length).toBe(2)
  state.releaseTrend()
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  await expect(page.getByTestId('resource-comparison')).toHaveCount(0)
  await expect(page).not.toHaveURL(/compare=/)
})


test('capacity gaps do not suppress a complete usage comparison or invent capacity values', async ({ page }) => {
  await resourceBackend(page, { trendResponse(body, params) {
    completeTrend(body, params)
    for (const line of body.series.filter(line => line.unit === 'bytes')) {
      Object.assign(line.points[1], { value: null, sampled_at: null, data_state: 'no_data' })
    }
    body.series.reverse()
  } })
  await page.goto(comparisonURL)
  await expect(page.getByTestId('resource-compare-change')).toHaveText('较上期上升 10.00 个百分点')
  await expect(page.getByTestId('resource-compare-current').locator('strong')).toHaveText('40.00 %')
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  await expect(page.getByTestId('resource-trend-help').getByRole('button')).toHaveAccessibleName(/右轴：内存使用率（%）/)
})
