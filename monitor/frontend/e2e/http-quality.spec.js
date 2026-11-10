import { expect, test } from '@playwright/test'
import { identity, setIdentity } from './monitoring-targets.fixture'
import { httpBackend, httpPermissions, rulePermissions } from './http-quality.fixture'

for (const locale of ['zh-cn', 'en']) test(`HTTP quality preserves zero traffic and separates route rankings (${locale})`, async ({ page }, testInfo) => {
  const state = await httpBackend(page, { locale })
  await page.goto('/http-quality?refresh=off&instance_id=1')
  const rows = page.getByTestId('http-instance')
  await expect(rows).toHaveCount(2)
  await expect(rows.nth(1).locator('[data-http-metric="http.requests.per_second"]')).toHaveText('0')
  await expect(rows.nth(1).locator('[data-http-metric="http.failures.percent"]')).toHaveText('—')
  const ranked = page.getByTestId('http-interfaces').locator('tbody tr')
  await expect(ranked.first()).toContainText('/slow/:id')
  await expect(page.getByTestId('http-trend').locator('canvas')).toBeVisible()
  await ranked.first().getByRole('button').click()
  await expect.poll(() => state.calls.filter(call => call.kind === 'http_trends').at(-1)?.route).toBe('/slow/:id')
  expect(state.calls.filter(call => call.kind === 'http_trends').at(-1)).toMatchObject({ method: 'GET', instance_ids: '1' })
  await page.screenshot({ path: testInfo.outputPath(`http-quality-${locale}.png`), fullPage: true })
})

test('HTTP monitoring groups instances and applies the System module and layer selections immediately', async ({ page }, testInfo) => {
  const backend = Array.from({ length: 11 }, (_, i) => ({
    id: i + 1, module_name: i < 2 ? 'meta' : 'monitor', instance_id: `http-${i + 1}`, role: 'backend',
    status: 'up', node_id: '', process_metrics_declared: true, process_started_at: '2026-10-19T23:00:00Z'
  }))
  const state = await httpBackend(page, { modules: ['asset', 'gateway', 'meta', 'monitor'], instances: [
    ...backend, { ...backend[0], id: 12, instance_id: 'gateway-1', module_name: 'gateway', role: 'ingress' }
  ] })
  await page.goto('/http-quality?refresh=off')
  await expect(page.getByRole('heading', { name: 'HTTP 监控' })).toBeVisible()
  const cards = page.getByTestId('http-module-card')
  const meta = cards.filter({ has: page.getByRole('heading', { name: 'meta', exact: true }) })
  await expect(cards).toHaveCount(2)
  await expect(meta.getByTestId('http-instance')).toHaveCount(2)
  const instance = meta.getByTestId('http-instance').first()
  await expect(instance.locator('[data-http-metric="http.duration.p95_seconds"]')).toBeVisible()
  await expect(instance.locator('[data-http-metric="http.active_requests"]')).toBeHidden()
  await instance.locator('summary').click()
  await expect(instance.locator('[data-http-metric="http.active_requests"]')).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('http-monitoring-cards.png'), fullPage: true })
  await page.setViewportSize({ width: 375, height: 800 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('http-monitoring-cards-narrow.png'), fullPage: true })

  await page.locator('.el-pagination .btn-next').click()
  await expect(page).toHaveURL(url => url.searchParams.get('page') === '2')
  const module = page.getByRole('combobox', { name: '模块名称', exact: true })
  await module.fill('ass')
  await page.getByRole('option', { name: 'asset', exact: true }).click()
  await expect(page).toHaveURL(url => url.searchParams.get('module_name') === 'asset' && !url.searchParams.has('page'))
  await expect(page.getByText('没有符合条件的当前实例', { exact: true })).toBeVisible()
  expect(state.calls.filter(call => call.kind === 'instances').at(-1)).toMatchObject({ module_name: 'asset', role: 'backend', page: '1' })

  await page.getByRole('button', { name: '重置', exact: true }).click()
  await page.getByRole('radiogroup', { name: '观测层' }).getByText('网关入口', { exact: true }).click()
  await expect(page.getByRole('radio', { name: '网关入口', exact: true })).toBeChecked()
  await expect(page).toHaveURL(url => url.searchParams.get('role') === 'ingress' && !url.searchParams.has('module_name'))
  await expect(cards).toHaveCount(1)
  await expect(cards).toContainText('gateway-1')
  expect(state.calls.filter(call => call.kind === 'instances').at(-1)).toMatchObject({ role: 'ingress', page: '1' })
})

test('HTTP automatic refresh keeps the card and expanded metrics until new evidence returns', async ({ page }) => {
  const state = await httpBackend(page)
  await page.clock.install()
  await page.goto('/http-quality')
  const instance = page.getByTestId('http-instance').first()
  await instance.locator('summary').click()
  const active = instance.locator('[data-http-metric="http.active_requests"]')
  await expect(active).toHaveText('1')
  state.hold()
  await page.clock.fastForward(15001)
  await expect.poll(() => state.calls.filter(call => call.kind === 'http_observations').length).toBe(2)
  await expect(active).toBeVisible()
  await expect(active).toHaveText('1')
  state.release()
  await expect(page.getByRole('button', { name: '刷新', exact: true })).toBeEnabled()
  await expect(active).toBeVisible()
})

test('HTTP quality discards foreign identity and stops automatic reads after permission denial', async ({ page }) => {
  const state = await httpBackend(page)
  await page.clock.install()
  await page.goto('/http-quality')
  await expect(page.getByTestId('http-instances')).toContainText('http-1')
  state.setMode('foreign')
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('http-error')).toBeVisible()
  await expect(page.getByTestId('http-instances')).not.toContainText('http-1')
  state.setMode('denied')
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('http-error')).toBeVisible()
  const count = state.calls.filter(call => call.kind === 'http_observations').length
  await page.clock.fastForward(60001)
  expect(state.calls.filter(call => call.kind === 'http_observations')).toHaveLength(count)
})

test('explicit offline history keeps the same instance and absolute trend after reload', async ({ page }) => {
  const state = await httpBackend(page)
  state.stop(1)
  await page.goto('/http-quality?status=down')
  await expect(page.getByTestId('http-instances')).toContainText('http-1')
  await expect(page.getByTestId('http-instances')).not.toContainText('http-2')
  await page.getByRole('button', { name: '停源前趋势', exact: true }).click()
  await expect(page.getByTestId('http-trend').locator('canvas')).toBeVisible()
  const fixed = { instance_ids: '1', start: '2026-10-19T23:00:00.000Z', end: '2026-10-20T00:00:00.000Z' }
  expect(state.calls.filter(call => call.kind === 'http_trends').at(-1)).toMatchObject(fixed)
  await page.reload()
  await expect(page.getByTestId('http-trend').locator('canvas')).toBeVisible()
  expect(state.calls.filter(call => call.kind === 'http_trends').at(-1)).toMatchObject(fixed)
  expect(state.calls.some(call => ['http_observations', 'http_interfaces'].includes(call.kind))).toBe(false)
  await setIdentity(page, identity([]))
  await expect(page.getByTestId('http-trend')).toHaveCount(0)
})

test('a pending HTTP response cannot return after current-user permissions are removed', async ({ page }) => {
  const state = await httpBackend(page)
  state.hold()
  await page.goto('/http-quality?refresh=off')
  await expect.poll(() => state.calls.some(call => call.kind === 'http_observations')).toBe(true)
  await setIdentity(page, identity([]))
  state.release()
  await expect(page.getByTestId('http-instances')).toHaveCount(0)
  await expect(page.getByTestId('http-quality')).not.toContainText('http-1')
})

for (const [name, context, principal] of [['tenant', { type: 'tenant', tenant_id: 1 }, 'user'], ['machine', { type: 'platform' }, 'service_principal']]) test(`HTTP quality rejects ${name} context before data reads`, async ({ page }) => {
  const state = await httpBackend(page, { identity: identity(httpPermissions, context, principal) })
  await page.goto('/http-quality?refresh=off')
  await expect(page.getByTestId('http-instances')).toHaveCount(0)
  expect(state.calls).toEqual([])
})

test('HTTP rules require explicit thresholds, create disabled, and leave CAS conflicts for review', async ({ page }) => {
  const state = await httpBackend(page, { identity: identity(rulePermissions) })
  await page.goto('/platform-alerts?tab=http-rules')
  await page.getByRole('button', { name: '新增接口规则', exact: true }).click()
  await page.getByRole('dialog').getByRole('textbox', { name: '名称', exact: true }).fill('Failure threshold')
  await page.locator('.el-select').filter({ has: page.getByRole('combobox', { name: '选择当前实例', exact: true }) }).click()
  await page.getByRole('option', { name: 'monitor · http-1', exact: true }).click()
  await page.getByRole('spinbutton', { name: '触发值', exact: true }).fill('30')
  await page.getByRole('spinbutton', { name: '恢复值', exact: true }).fill('10')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect.poll(() => state.writes.length).toBe(1)
  expect(state.writes[0]).toMatchObject({ method: 'POST', input: { enabled: false, instance_id: 1, trigger_value: 30, recovery_value: 10, route: null, method: '' } })
  await expect(page.getByTestId('http-rules')).toContainText('Failure threshold')
  await page.getByTestId('http-rules').getByRole('button', { name: '编辑', exact: true }).click()
  await page.locator('.el-select').filter({ has: page.getByRole('combobox', { name: '选择当前实例', exact: true }) }).click()
  await page.getByRole('option', { name: 'monitor · http-2', exact: true }).click()
  state.setConflict(true)
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect.poll(() => state.writes.length).toBe(2)
  expect(state.writes[1].input.version).toBe(1)
  await expect(page.getByRole('dialog')).toContainText('版本')
  await expect(page.getByRole('dialog')).toBeVisible()
})

test('statistics permission does not grant HTTP rule management', async ({ page }) => {
  const state = await httpBackend(page)
  await page.goto('/platform-alerts?tab=http-rules')
  await expect(page.getByTestId('http-rules')).toHaveCount(0)
  expect(state.calls.filter(call => call.kind === 'rules')).toEqual([])
})

test('read-only HTTP rules mark a sustained slow template without loading System instances', async ({ page }) => {
  const state = await httpBackend(page, { identity: identity(['monitor.platform_http_rule.read']), rule: {
    id: '10000000-0000-4000-8000-000000000019', version: 2, name: 'Slow template', anchor_instance_id: 1,
    module_name: 'monitor', role: 'backend', route: '/slow/:id', method: 'GET', metric_key: 'http.duration.mean_seconds',
    trigger_value: 1, recovery_value: .5, trigger_seconds: 30, recovery_seconds: 30, enabled: true,
    last_verdict: 'risk', last_error: '', last_value: 2, last_evaluated_at: '2026-10-20T00:00:00Z'
  } })
  await page.goto('/platform-alerts?tab=http-rules')
  await expect(page.getByTestId('http-rules')).toContainText('持续慢接口')
  await expect(page.getByTestId('http-rules').getByRole('button', { name: '编辑', exact: true })).toHaveCount(0)
  expect(state.calls.filter(call => call.kind === 'instances')).toEqual([])
})
