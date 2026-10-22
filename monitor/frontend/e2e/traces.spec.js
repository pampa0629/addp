import { test, expect } from '@playwright/test'
import { identity, setIdentity } from './monitoring-targets.fixture'
import { tracesBackend as backend, tracePermissions as keys, traceQuery as query } from './traces.fixture'
for (const locale of ['zh-cn','en']) test(`Trace detail shows sampled partial evidence and actual edges (${locale})`, async ({ page }, info) => {
  await backend(page, { locale }); await page.goto('/traces?'+query())
  await expect(page.getByTestId('trace-spans')).toContainText('/items/:id')
  await expect(page.getByTestId('trace-edges')).toContainText('system')
  await expect(page.getByTestId('trace-edges').locator('tbody tr')).toHaveCount(1)
  await expect(page.getByRole('button', { name: locale === 'en' ? 'Related logs' : '关联日志', exact: true })).toHaveCount(0)
  await expect(page.getByTestId('traces')).not.toContainText('monitor.traces.')
  await page.screenshot({ path: info.outputPath(`traces-${locale}.png`), fullPage: true })
})
test('Trace failure clears prior data and invalid projections cannot retain it', async ({ page }) => {
  const state = await backend(page); await page.goto('/traces?'+query()); await expect(page.getByTestId('trace-spans')).toContainText('/items/:id')
  state.mode('foreign'); await page.getByRole('button', { name: '刷新', exact: true }).click(); await expect(page.getByTestId('trace-error')).toBeVisible(); await expect(page.getByTestId('trace-spans')).toHaveCount(0)
  state.mode('failed'); await page.getByRole('button', { name: '刷新', exact: true }).click(); await expect(page.getByTestId('trace-spans')).toHaveCount(0)
})
test('A pending Trace response cannot restore data after current User revocation', async ({ page }) => {
  const state = await backend(page); state.hold(); await page.goto('/traces?'+query()); await expect.poll(() => state.calls.length).toBe(1)
  await setIdentity(page, identity([])); state.release(); await expect(page.getByTestId('trace-spans')).toHaveCount(0); await expect(page.getByTestId('traces')).not.toContainText('/items/:id')
})
for (const [name, context, principal] of [['tenant',{type:'tenant',tenant_id:1},'user'],['machine',{type:'platform'},'service_principal']]) test(`Trace page denies ${name} before reading telemetry`, async ({ page }) => {
  const state = await backend(page,{identity:identity(keys,context,principal)});await page.goto('/traces?'+query());await expect(page.getByTestId('trace-spans')).toHaveCount(0);expect(state.calls).toEqual([])
})

for (const locale of ['zh-cn','en']) test(`Execution phase links open an independently authorized target window (${locale})`, async ({ page }, info) => {
  const state = await backend(page, { locale, links: true }); await page.goto('/traces?'+query())
  await expect(page.getByTestId('trace-spans')).toContainText(locale === 'en' ? 'Terminal' : '终态')
  await expect(page.getByTestId('trace-spans')).toContainText(locale === 'en' ? 'Failed' : '失败')
  await expect(page.getByTestId('trace-links').locator('tbody tr')).toHaveCount(1)
  await expect(page.getByTestId('trace-edges')).toContainText(locale === 'en' ? 'No matched calls' : '没有已匹配的调用关系')
  await page.screenshot({ path: info.outputPath(`execution-links-${locale}.png`), fullPage: true })
  await page.getByRole('button', { name: locale === 'en' ? 'Follow link' : '查看关联', exact: true }).click()
  await expect.poll(() => state.calls.length).toBe(2)
  const target = state.calls[1]
  expect(target.instance_id).toBe('2')
  expect(Date.parse(target.start)).toBe(Date.parse(state.targetStart)-1000)
  expect(Date.parse(target.end)).toBe(Date.parse(state.targetStart)+1000)
  await expect(page.getByTestId('trace-spans')).toContainText('/items/:id')
  await expect(page).toHaveURL(new RegExp('trace_id='+'a'.repeat(32)))
})
test('Link target denial clears the source detail and all causal links', async ({ page }) => {
  await backend(page, { links: true, targetDenied: true }); await page.goto('/traces?'+query())
  await expect(page.getByTestId('trace-links')).toContainText('system')
  await page.getByRole('button', { name: '查看关联', exact: true }).click()
  await expect(page.getByTestId('trace-error')).toBeVisible()
  await expect(page.getByTestId('trace-spans')).toHaveCount(0)
  await expect(page.getByTestId('trace-links')).toHaveCount(0)
})
test('Current User revocation clears execution stages and causal links', async ({ page }) => {
  await backend(page, { links: true }); await page.goto('/traces?'+query())
  await expect(page.getByTestId('trace-links')).toContainText('system')
  await setIdentity(page, identity([]))
  await expect(page.getByTestId('trace-spans')).toHaveCount(0)
  await expect(page.getByTestId('trace-links')).toHaveCount(0)
})
