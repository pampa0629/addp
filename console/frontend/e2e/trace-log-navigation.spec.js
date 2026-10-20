import { test, expect } from '@playwright/test'
import { tracesBackend, tracePermissions, traceQuery } from '../../../monitor/frontend/e2e/traces.fixture'
import { identity } from '../../../monitor/frontend/e2e/monitoring-targets.fixture'
const traceID = '1'.repeat(32)
async function fixture(page) {
  const state = await tracesBackend(page, { identity: identity([...tracePermissions,'platform.module_log.read']), longSpan: true })
  await page.route('**/api/v1/system/platform/modules', route => route.fulfill({ json: { modules: [{ id: 1, module_name: 'monitor', route_prefix: '/monitor', enabled: true, version: 1, instances: [] }] } }))
  const logs = []
  await page.route('**/api/v1/system/platform/modules/*/instances/*/logs?**', async route => {
    const q = Object.fromEntries(new URL(route.request().url()).searchParams);logs.push(q)
    const timestamp = new Date((Date.parse(q.from)+Date.parse(q.to))/2).toISOString()
    await route.fulfill({ json: { entries: [{ entry_id: 'one:1', timestamp, level: 'info', channel: 'stdout', message: 'linked log evidence', trace_id: traceID, span_id: '2'.repeat(16) }], returned: 1, queried_at: new Date().toISOString(), collection_state: 'unknown', has_more: false, next_cursor: '', from: q.from, to: q.to, outside_retention: false } })
  })
  return { ...state, logs }
}
test('Console restores a Trace, opens exact System logs, and returns through one authorized module iframe', async ({ page }, info) => {
  const state = await fixture(page);await page.goto('/monitor/traces?'+traceQuery())
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(frame.getByTestId('trace-spans')).toContainText('/items/:id')
  const initial = page.url();await page.reload();await expect(frame.getByTestId('trace-spans')).toContainText('/items/:id');expect(page.url()).toBe(initial)
  await frame.getByTestId('trace-spans').getByRole('button',{name:'关联日志',exact:true}).first().click()
  await expect(page).toHaveURL(url => url.pathname === '/system/modules' && url.searchParams.get('trace_id') === traceID && url.searchParams.get('log_instance') === 'one')
  await expect(frame.getByTestId('runtime-log-output')).toContainText('linked log evidence')
  expect(state.logs.at(-1).trace_id).toBe(traceID)
  expect(Date.parse(state.logs.at(-1).to)-Date.parse(state.logs.at(-1).from)).toBeLessThanOrEqual(3600000)
  const logURL = page.url();await page.reload();await expect(frame.getByTestId('runtime-log-output')).toContainText('linked log evidence');expect(page.url()).toBe(logURL)
  await frame.getByRole('dialog').getByRole('button',{name:'查询调用链',exact:true}).click()
  await expect(page).toHaveURL(url => url.pathname === '/monitor/traces' && url.searchParams.get('trace_id') === traceID)
  await expect(frame.getByTestId('trace-spans')).toContainText('/items/:id')
  await expect(page.locator('iframe[data-testid="module-iframe"]')).toHaveCount(1)
  await page.screenshot({path:info.outputPath('trace-system-log-return.png'),fullPage:true})
})
test('Tenant permission strings cannot mount a platform Trace iframe', async ({ page }) => {
  const state = await tracesBackend(page,{identity:identity(tracePermissions,{type:'tenant',tenant_id:1})})
  await page.goto('/monitor/traces?'+traceQuery());await expect(page.locator('iframe[data-testid="module-iframe"]')).toHaveCount(0);expect(state.calls).toEqual([])
})
