import { expect, test } from '@playwright/test'
import { httpResponses, assertHTTPPage, assertHTTPRestored, assertHTTPStoppedPage } from './http-quality-page-support.mjs'
import { httpInstances, httpBackend, httpPermissions } from '../../../monitor/frontend/e2e/http-quality.fixture'
import { identity } from '../../../monitor/frontend/e2e/monitoring-targets.fixture'

test('HTTP quality keeps one Console iframe while selecting a template and restoring a deep link', async ({ page }) => {
  await httpBackend(page)
  await page.goto('/monitor/http-quality?refresh=off')
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(frame.getByRole('heading', { name: 'HTTP 监控' })).toBeVisible()
  await expect(frame.getByTestId('http-instances')).toContainText('http-1')
  const origin = await frame.locator('body').evaluate(() => performance.timeOrigin)
  await frame.getByTestId('http-instances').getByRole('button', { name: '接口与趋势', exact: true }).first().click()
  await expect(page).toHaveURL(url => url.searchParams.get('instance_id') === '1')
  await frame.getByTestId('http-interfaces').getByRole('button', { name: 'GET /slow/:id', exact: true }).click()
  await expect(page).toHaveURL(url => url.searchParams.get('route') === '/slow/:id' && url.searchParams.get('method') === 'GET')
  await expect(frame.getByTestId('http-trend').locator('canvas')).toBeVisible()
  expect(await frame.locator('body').evaluate(() => performance.timeOrigin)).toBe(origin)
  const url = page.url(); await page.reload()
  await expect(frame.getByTestId('http-trend').locator('canvas')).toBeVisible()
  expect(page.url()).toBe(url)
})

test('selected HTTP interface opens the 5xx sample for the same window and distinguishes its downstream failure', async ({ page }) => {
  const permissions = [...httpPermissions, 'monitor.trace.read', 'platform.module_log.read']
  const state = await httpBackend(page, { identity: identity(permissions) })
  const traceID = '1'.repeat(32)
  const calls = []
  await page.route('**/api/v1/monitor/platform/traces**', route => {
    const params = Object.fromEntries(new URL(route.request().url()).searchParams)
    calls.push(params)
    const { start, end } = params
    const stopped = new Date(Date.parse(start) + 500).toISOString()
    const selected = { module_name: 'monitor', instance_id: 'http-1', role: 'backend', scope: 'addp.http', kind: 'server',
      span_id: '2'.repeat(16), parent_span_id: '', attempt: 0, links: [], method: 'GET', route: '/slow/:id',
      status: 503, failed: true, start, end: stopped, duration_ms: 500 }
    const client = { ...selected, span_id: '3'.repeat(16), parent_span_id: selected.span_id, kind: 'client', route: '' }
    const downstream = { ...selected, span_id: '4'.repeat(16), parent_span_id: client.span_id,
      module_name: 'system', instance_id: 'system-1', route: '/system/check', status: 500, duration_ms: 400 }
    const edge = { client_span_id: client.span_id, server_span_id: downstream.span_id,
      from_module: client.module_name, from_instance: client.instance_id, to_module: downstream.module_name, to_instance: downstream.instance_id }
    return route.fulfill({ json: { start, end, queried_at: end, policy_version: 2, coverage: 'sampled',
      data: [{ trace_id: traceID, start, end: stopped, duration_ms: 500, partial: false, policy_version: 2, coverage: 'sampled',
        edges: [edge], spans: [selected, client, downstream] }]
    } })
  })
  await page.goto('/monitor/http-quality?instance_id=1&range=5m&refresh=off')
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  await frame.getByRole('button', { name: 'GET /slow/:id', exact: true }).click()
  await frame.getByRole('button', { name: '查看该接口的 5xx 调用链' }).click()
  const trend = state.calls.findLast(call => call.kind === 'http_trends' && call.route === '/slow/:id')
  const expected = { instance_id: '1', http_method: 'GET', http_route: '/slow/:id', start: trend.start, end: trend.end }
  await expect.poll(() => calls.at(-1)).toEqual(expected)
  await expect(page).toHaveURL(url => url.pathname === '/monitor/traces' && Object.entries(expected).every(([key, value]) => url.searchParams.get(key) === value))
  await frame.getByRole('button', { name: '查看调用链', exact: true }).click()
  const failure = frame.getByTestId('trace-failure')
  const downstreamFailure = frame.getByTestId('trace-downstream-failure')
  await expect(failure).toContainText('所选接口的 5xx')
  await expect(failure).toContainText('GET /slow/:id')
  await expect(failure).toContainText('HTTP 503')
  await expect(downstreamFailure).toContainText('已观测到的下游失败')
  await expect(downstreamFailure).toContainText('GET /system/check')
  await expect(downstreamFailure).toContainText('HTTP 500')
  await expect(downstreamFailure).toContainText('观测到的最长下游调用')
  await expect(frame.getByTestId('trace-downstream')).toHaveCount(0)
  expect(calls.at(-1)).toEqual(expected)
  await failure.getByRole('button', { name: '定位到时间线' }).click()
  await expect(frame.locator('#span-' + '2'.repeat(16))).toBeFocused()
  await downstreamFailure.getByRole('button', { name: '定位到时间线' }).click()
  await expect(frame.locator('#span-' + '4'.repeat(16))).toBeFocused()
  await failure.getByRole('button', { name: '关联日志' }).click()
  await expect(page).toHaveURL(url => url.pathname === '/system/modules' && url.searchParams.get('trace_id') === traceID && url.searchParams.get('log_instance') === 'http-1')
  await page.goBack()
  await downstreamFailure.getByRole('button', { name: '关联日志' }).click()
  await expect(page).toHaveURL(url => url.pathname === '/system/modules' && url.searchParams.get('trace_id') === traceID && url.searchParams.get('log_instance') === 'system-1')
})

test('Tenant HTTP permission strings cannot mount the platform quality iframe', async ({ page }) => {
  const state = await httpBackend(page, { identity: identity(httpPermissions, { type: 'tenant', tenant_id: 1 }) })
  await page.goto('/monitor/http-quality')
  await expect(page.locator('iframe[data-testid="module-iframe"]')).toHaveCount(0)
  expect(state.calls).toEqual([])
})

const products = httpInstances.map((row, index) => ({ ...row, module_name: index ? 'copilot' : 'agent' }))
const productQuery = { module_name: 'agent', refresh: 'off', instance_id: '1', range: '5m', sort: 'qps' }

test('formal HTTP page oracles compare actual cells and tooltip values, restore both URLs and reject corruption', async ({ page }) => {
  await httpBackend(page, { instances: products })
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]'), owner = products[0]
  const payloads = await httpResponses(page, () => page.goto('/monitor/http-quality?' + new URLSearchParams(productQuery)), { owner })
  await assertHTTPPage(expect, frame, owner, payloads, { sort: 'qps' })
  const query = { ...productQuery, route: '/slow/:id', method: 'GET' }
  const filtered = await httpResponses(page, () => frame.getByTestId('http-interfaces').getByRole('button', { name: 'GET /slow/:id', exact: true }).click(), { owner })
  await assertHTTPRestored(expect, page, frame, query)
  await assertHTTPPage(expect, frame, owner, filtered, { sort: 'qps' })
  const restored = await httpResponses(page, () => page.reload(), { owner })
  await assertHTTPRestored(expect, page, frame, query)
  await assertHTTPPage(expect, frame, owner, restored, { sort: 'qps' })
  const short = expect.configure({ timeout: 200 })
  const qps = frame.getByTestId('http-instance').locator('[data-http-metric="http.requests.per_second"]')
  await qps.evaluate(element => { element.textContent = '999' })
  await expect(assertHTTPPage(short, frame, owner, restored, { sort: 'qps' })).rejects.toThrow()
  await qps.evaluate(element => { element.textContent = '10' })
  const foreign = structuredClone(restored); foreign[0].data[0].subject.instance_id = 'foreign'
  await expect(assertHTTPPage(short, frame, owner, foreign)).rejects.toThrow()
  const rankedCell = frame.getByTestId('http-interfaces').locator('tbody tr').first().locator('td').nth(1)
  const originalRanked = await rankedCell.innerText()
  await rankedCell.evaluate(element => { element.textContent = '777' })
  await expect(assertHTTPPage(short, frame, owner, restored, { sort: 'qps' })).rejects.toThrow()
  await rankedCell.evaluate((element, text) => { element.textContent = text }, originalRanked)
  const wrongTrend = structuredClone(restored)
  wrongTrend[2].data[0].observations[0].series[0].points.forEach(point => { point.value = 98765 })
  await expect(assertHTTPPage(short, frame, owner, wrongTrend, { sort: 'qps' })).rejects.toThrow()
  const empty = structuredClone(restored)
  empty[2].data[0].observations[0].series[0].points.forEach(point => { point.data_state = 'no_data'; point.value = null })
  await expect(assertHTTPPage(short, frame, owner, empty, { sort: 'qps' })).rejects.toThrow()
})

test('stopped HTTP source clears previously displayed values and rejects stale surviving Agent samples', async ({ page }) => {
  const state = await httpBackend(page, { instances: products })
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]'), owner = products[1]
  const path = '/monitor/http-quality?' + new URLSearchParams({ ...productQuery, module_name: 'copilot', instance_id: '2' })
  const live = await httpResponses(page, () => page.goto(path), { owner })
  await assertHTTPPage(expect, frame, owner, live, { sort: 'qps' })
  const missingCell = frame.getByTestId('http-instance').locator('[data-http-metric="http.failures.percent"]')
  await missingCell.evaluate(element => { element.textContent = '0' })
  await expect(assertHTTPPage(expect.configure({ timeout: 200 }), frame, owner, live, { sort: 'qps' })).rejects.toThrow()
  await missingCell.evaluate(element => { element.textContent = '—' })
  await expect(frame.getByTestId('http-trend').locator('canvas')).toBeVisible()
  await expect(frame.getByTestId('http-interfaces').locator('tbody tr')).toHaveCount(2)
  const origin = await frame.locator('body').evaluate(() => performance.timeOrigin)
  state.stop(owner.id)
  const listed = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/system/platform/module-instances' && response.ok())
  // A sort change reloads the current query within the open dialog; it must
  // clear the old metrics without destroying the existing iframe/component.
  await frame.locator('.el-select').filter({ has: frame.locator('[aria-label="排行依据"]') }).click()
  await frame.getByRole('option', { name: '平均耗时（秒）', exact: true }).click()
  expect((await (await listed).json()).data).toEqual([])
  await assertHTTPStoppedPage(expect, frame, owner)
  expect(await frame.locator('body').evaluate(() => performance.timeOrigin)).toBe(origin)
  const agent = await httpResponses(page, () => page.goto('/monitor/http-quality?' + new URLSearchParams(productQuery)), { owner: products[0] })
  await assertHTTPPage(expect, frame, products[0], agent, { sort: 'qps', stoppedAt: '2026-10-19T00:00:00Z' })
  await expect(assertHTTPPage(expect.configure({ timeout: 200 }), frame, products[0], agent, { sort: 'qps', stoppedAt: '2026-10-21T00:00:00Z' })).rejects.toThrow()
})
