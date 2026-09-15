import { expect, test } from '@playwright/test'

async function openPolicy(page, scope = 'platform', { locale = 'zh-cn', readonly = false, policy = {} } = {}) {
 await page.addInitScript(locale => localStorage.setItem('addp-lang', locale), locale)
 let saved = { version: 1, inherited: scope === 'tenant', default_query_timeout: 30, max_query_timeout: 300, query_result_limit: 500, query_concurrency: 20, query_per_engine_concurrency: 5, ...policy }
 const writes = []
 await page.route('**/develop/settings/query-policy', async route => {
  if (route.request().method() === 'PUT') {
   const input = route.request().postDataJSON()
   writes.push(input)
   saved = { ...saved, ...input, inherited: false, version: saved.version + 1 }
  }
  await route.fulfill({ json: saved })
 })
 await page.goto(`/e2e/fixtures/query-policy.html?scope=${scope}&readonly=${readonly}`)
 await expect(page.getByRole('spinbutton').first()).toHaveValue('30')
 return writes
}

test('platform saves hot concurrency with the version and keeps the form within a narrow viewport', async ({ page }) => {
 await page.setViewportSize({ width: 480, height: 900 })
 const writes = await openPolicy(page)
 const fields = page.getByRole('spinbutton')
 await expect(fields).toHaveCount(5)
 await expect(fields.nth(3)).toHaveValue('20')
 await expect(fields.nth(4)).toHaveValue('5')
 await fields.nth(3).fill('12')
 await fields.nth(4).fill('3')
 await page.getByRole('button', { name: '保存', exact: true }).click()
 await expect.poll(() => writes.length).toBe(1)
 expect(writes[0]).toEqual({ version: 1, default_query_timeout: 30, max_query_timeout: 300, query_result_limit: 500, query_concurrency: 12, query_per_engine_concurrency: 3 })
 await expect(page.getByText('保存后无需重启。', { exact: false })).toBeVisible()
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('tenant edits only its timeout and does not submit shared capacity', async ({ page }) => {
 await page.setViewportSize({ width: 480, height: 900 })
 const writes = await openPolicy(page, 'tenant')
 await expect(page.getByRole('spinbutton')).toHaveCount(1)
 await expect(page.getByText('已保存配置来源：继承平台默认', { exact: true })).toBeVisible()
 const constraints = page.getByRole('region', { name: '平台统一约束（只读）' })
 await expect(constraints).toBeVisible()
 await expect(constraints.getByRole('spinbutton')).toHaveCount(0)
 for (const [label, value] of [
  ['最大查询超时（秒）', '300'], ['结果预览上限（行）', '500'],
  ['总查询并发上限（每个 Backend）', '20'], ['单引擎查询并发上限', '5']
 ]) {
  await expect(constraints.getByRole('row').filter({ hasText: label }).locator('td').last()).toHaveText(value)
 }
 await expect(constraints.getByText('请联系平台管理员。', { exact: false })).toBeVisible()
 await expect(constraints.getByText('不是本租户独享配额。', { exact: false })).toBeVisible()
 await expect(page.getByText('默认查询超时保存后无需重启', { exact: false })).toBeVisible()
 await expect(page.getByText('并发调整作用于后续调度', { exact: false })).toHaveCount(0)
 await page.getByRole('spinbutton').fill('60')
 await page.getByRole('button', { name: '保存', exact: true }).click()
 await expect.poll(() => writes.length).toBe(1)
 expect(writes[0]).toEqual({ version: 1, default_query_timeout: 60 })
 await expect(page.getByText('已保存配置来源：本租户已设置', { exact: true })).toBeVisible()
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('tenant refresh reads changed platform constraints without issuing writes', async ({ page }) => {
 const writes = await openPolicy(page, 'tenant')
 await page.route('**/develop/settings/query-policy', route => {
  if (route.request().method() !== 'GET') writes.push(route.request().postDataJSON())
  return route.fulfill({ json: {
   version: 1, inherited: false, default_query_timeout: 60, max_query_timeout: 120,
   query_result_limit: 250, query_concurrency: 12, query_per_engine_concurrency: 3
  } })
 })
 await page.getByRole('button', { name: '刷新', exact: true }).click()
 await expect(page.getByRole('spinbutton')).toHaveValue('60')
 const values = page.getByRole('region', { name: '平台统一约束（只读）' }).locator('.el-descriptions__content')
 await expect(values).toHaveText(['120', '250', '12', '3'])
 expect(writes).toEqual([])
})

test('tenant with read permission sees constraints but cannot edit or save', async ({ page }) => {
 const writes = await openPolicy(page, 'tenant', { readonly: true })
 await expect(page.getByRole('region', { name: '平台统一约束（只读）' })).toBeVisible()
 await expect(page.getByRole('spinbutton')).toBeDisabled()
 await expect(page.getByRole('button', { name: '保存', exact: true })).toBeDisabled()
 expect(writes).toEqual([])
})

test('tenant constraints and ownership hints are localized and fit all themes', async ({ page }) => {
 await page.setViewportSize({ width: 480, height: 900 })
 await openPolicy(page, 'tenant', { locale: 'en', policy: { inherited: false } })
 const constraints = page.getByRole('region', { name: 'Platform constraints (read-only)' })
 await expect(constraints).toBeVisible()
 await expect(page.getByText('Saved configuration source: tenant override', { exact: true })).toBeVisible()
 await expect(constraints.getByText('contact your platform administrator.', { exact: false })).toBeVisible()
 for (const theme of ['', 'dark', 'dark blue', 'dark purple']) {
  await page.evaluate(theme => { document.documentElement.className = theme }, theme)
  await expect(constraints.getByText('500', { exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
 }
})

test('failed initial load does not show frontend defaults as platform constraints', async ({ page }) => {
 await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
 await page.route('**/develop/settings/query-policy', route => route.fulfill({ status: 503, json: { error: '读取配置失败' } }))
 await page.goto('/e2e/fixtures/query-policy.html?scope=tenant')
 await expect(page.getByText('读取配置失败', { exact: true })).toBeVisible()
 await expect(page.getByRole('region', { name: '平台统一约束（只读）' })).toHaveCount(0)
 await expect(page.getByRole('spinbutton')).toHaveCount(0)
})
