import { expect, test } from '@playwright/test'
import { identity, setIdentity } from './monitoring-targets.fixture'
import { httpBackend, httpPermissions, rulePermissions } from './http-quality.fixture'

for (const locale of ['zh-cn', 'en']) test(`HTTP quality preserves zero traffic and separates route rankings (${locale})`, async ({ page }, testInfo) => {
  const state = await httpBackend(page, { locale })
  await page.goto('/http-quality?refresh=off&instance_id=1')
  const rows = page.getByTestId('http-instances').locator('tbody tr')
  await expect(rows).toHaveCount(2)
  await expect(rows.nth(1)).toContainText('0')
  await expect(rows.nth(1)).toContainText('—')
  const ranked = page.getByTestId('http-interfaces').locator('tbody tr')
  await expect(ranked.first()).toContainText('/slow/:id')
  await expect(page.getByTestId('http-trend').locator('canvas')).toBeVisible()
  await ranked.first().getByRole('button').click()
  await expect.poll(() => state.calls.filter(call => call.kind === 'http_trends').at(-1)?.route).toBe('/slow/:id')
  expect(state.calls.filter(call => call.kind === 'http_trends').at(-1)).toMatchObject({ method: 'GET', instance_ids: '1' })
  await page.screenshot({ path: testInfo.outputPath(`http-quality-${locale}.png`), fullPage: true })
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
