import { expect, test } from '@playwright/test'
import { identity, setIdentity } from './monitoring-targets.fixture'
import { overviewBackend, overviewPermissions } from './resource-overview.fixture'

for (const locale of ['zh-cn', 'en']) {
  test(`overview full scope, coverage and valid zero in ${locale} on a narrow screen`, async ({ page }) => {
    const state = await overviewBackend(page, { locale }); await page.setViewportSize({ width: 620, height: 850 })
    await page.goto('/resource-overview?refresh=off')
    await expect(page.getByTestId('overview-statistics')).toContainText('43')
    await expect(page.getByTestId('overview-rank-0')).toContainText('完整范围主机41')
    await expect(page.getByTestId('overview-rank-0')).toContainText('0.00 %')
    await expect(page.getByTestId('overview-rank-0')).toContainText(locale === 'en' ? 'Incomplete coverage' : '覆盖不完整')
    await expect(page.getByTestId('overview-rank-0')).toContainText('5 /')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    expect(state.calls).toEqual([{ method: 'GET', query: '', body: null }])
  })
}
test('overview failure clears old rankings, budget is explicit, and empty is distinct', async ({ page }) => {
  const state = await overviewBackend(page); await page.goto('/resource-overview?refresh=off')
  await expect(page.getByTestId('overview-rank-0')).toBeVisible()
  for (const mode of ['budget', 'malformed', 'unavailable']) {
    state.mode(mode); await page.getByRole('button', { name: '刷新', exact: true }).click()
    await expect(page.getByTestId('overview-error')).toBeVisible(); await expect(page.getByTestId('overview-rank-0')).toHaveCount(0)
    if (mode === 'budget') await expect(page.getByTestId('overview-error')).toContainText('未生成部分榜单')
  }
  state.mode('empty'); await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('overview-rank-0')).toContainText('没有纳管主机')
})
test('overview natural refresh, off reload, and denied stop consume no paging parameters', async ({ page }) => {
  const state = await overviewBackend(page); await page.clock.install(); await page.goto('/resource-overview')
  await expect(page.getByTestId('overview-rank-0')).toBeVisible(); state.stamp('2026-10-13T00:00:15Z')
  await page.clock.fastForward(15001); await expect.poll(() => state.calls.length).toBe(2)
  await expect(page.getByTestId('overview-time')).toContainText(':00:15')
  state.mode('denied'); await page.clock.fastForward(15001)
  await expect(page.getByTestId('overview-error')).toBeVisible(); await expect(page.getByTestId('overview-rank-0')).toHaveCount(0)
  await page.clock.fastForward(60001); expect(state.calls).toHaveLength(3)
  state.mode('valid'); await page.getByRole('button', { name: '刷新', exact: true }).click(); await expect(page.getByTestId('overview-rank-0')).toBeVisible()
  await page.getByTestId('overview-refresh').click(); await page.getByRole('option', { name: '关闭自动刷新', exact: true }).click()
  await expect(page).toHaveURL(/refresh=off/); await page.reload(); await expect(page.getByTestId('overview-rank-0')).toBeVisible()
  const count = state.calls.length; await page.clock.fastForward(120001); expect(state.calls).toHaveLength(count)
  expect(state.calls.every(call => !call.query && !call.body)).toBe(true)
})
test('overview slow read is cancelled on hide, resumes, and context change rejects late data', async ({ page }) => {
  const state = await overviewBackend(page); await page.clock.install(); await page.goto('/resource-overview')
  await expect(page.getByTestId('overview-rank-0')).toBeVisible(); state.hold(); await page.clock.fastForward(15001); await expect.poll(() => state.calls.length).toBe(2)
  await page.clock.fastForward(60001); expect(state.calls).toHaveLength(2)
  await page.evaluate(() => { Object.defineProperty(document, 'hidden', { configurable: true, value: true }); document.dispatchEvent(new Event('visibilitychange')) })
  state.release(); await page.clock.fastForward(60001); expect(state.calls).toHaveLength(2)
  await page.evaluate(() => { Object.defineProperty(document, 'hidden', { configurable: true, value: false }); document.dispatchEvent(new Event('visibilitychange')) })
  await expect.poll(() => state.calls.length).toBe(3); await expect(page.getByRole('button', { name: '刷新', exact: true })).toBeEnabled()
  state.hold(); await page.getByRole('button', { name: '刷新', exact: true }).click(); await expect.poll(() => state.calls.length).toBe(4)
  await setIdentity(page, identity(overviewPermissions, { type: 'tenant', tenant_id: '7' })); state.release()
  await expect(page.getByTestId('overview-denied')).toBeVisible(); await expect(page.getByTestId('overview-rank-0')).toHaveCount(0)
  await page.clock.fastForward(60001); expect(state.calls).toHaveLength(4)
})
test('overview refuses tenant, machine, delegated and either missing owner permission without requests', async ({ page }) => {
  for (const auth of [identity(overviewPermissions, { type: 'tenant', tenant_id: '7' }), identity(overviewPermissions, { type: 'platform' }, 'service_principal'), identity(overviewPermissions, { type: 'platform' }, 'user', {}), identity(['monitor.resource_observation.read']), identity(['platform.host_node.read'])]) {
    const state = await overviewBackend(page, { identity: auth }); await page.goto('/resource-overview')
    await expect(page.getByTestId('overview-denied').or(page.locator('.el-result'))).toBeVisible(); expect(state.calls).toHaveLength(0)
    await page.unrouteAll({ behavior: 'wait' })
  }
})
