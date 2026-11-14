import { test, expect } from '@playwright/test'
import { developAuthContext } from './developAuthContext.js'

const ID = '11111111-1111-4111-8111-111111111111'
const rows = Array.from({ length: 50 }, (_, i) => ({ instance_id: i + 1, score: 0.9, footprint: (i + 1) * 10.25 }))
const sum = rows.reduce((value, row) => value + row.footprint, 0)
const history = id => ({ execution_id: id, module: 'develop', task_type: 'workflow', status: 'success', progress: 100,
  metadata: { result: { final_result: { runtime_type: 'dataframe', row_count: 50 } } }, outputs: {},
  execution_config: { content: { workflow_definition: { tasks: [] } } } })
const live = (id, deadline) => ({ ...history(id), metadata: { result: { final_result: {
  schema_version: 'addp.workflow.table-result/v1', columns: ['instance_id', 'score', 'footprint'], rows, row_count: 50
}, professional_result_expires_at: deadline } } })
const json = (route, body, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
async function fixture(page, locale = 'zh-cn', resultHandler) {
  await page.addInitScript(locale => localStorage.setItem('addp-lang', locale), locale)
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/v1/system/refresh') return json(route, { access_token: 'isolated-browser-fixture', expires_in: 3600 })
    if (path === '/api/v1/system/users/me') return json(route, { id: 1, username: 'fixture' })
    if (path === '/api/v1/system/auth/context') return json(route, developAuthContext)
    if (path === '/api/v1/meta/engines') return json(route, [])
    if (path.endsWith('/result')) return resultHandler(route)
    if (path.endsWith('/logs')) return json(route, { logs: [] })
    if (path.startsWith('/api/v1/develop/executions/')) return json(route, history(path.split('/').pop()))
    return json(route, {})
  })
}
for (const locale of ['zh-cn', 'en']) test(`professional workflow rows, explicit sum, pagination and clearing in ${locale}`, async ({ page }, testInfo) => {
  const errors = []; page.on('pageerror', e => errors.push(e.message))
  let requests = 0
  const deadline = new Date(Date.now() + 60000).toISOString()
  await fixture(page, locale, route => { requests++; return json(route, live(ID, deadline)) })
  await page.goto(`/executions/${ID}`)
  const section = page.locator('.workflow-professional-result')
  const read = page.getByRole('button', { name: locale === 'en' ? 'View current details' : '查看当次明细', exact: true })
  await expect(read).toBeVisible(); expect(requests).toBe(0)
  await read.click()
  await expect(section.locator('.query-result')).toBeVisible()
  await expect(section).toContainText('10.25')
  await section.locator('.sum-column-select').click()
  await page.getByRole('option', { name: 'footprint', exact: true }).click()
  await expect(section.getByTestId('professional-sum')).toContainText(String(sum))
  await expect(section.getByTestId('professional-sum')).not.toContainText('m²')
  const next = section.locator('.btn-next'); await next.click()
  await expect(section.locator('.tabular-result-renderer')).toContainText('30')
  await expect(section.getByTestId('professional-sum')).toContainText(String(sum))
  await expect(section.getByRole('button', { name: /下载|导出|Download|Export/ })).toHaveCount(0)
  await page.screenshot({ path: testInfo.outputPath(`workflow-professional-${locale}-desktop.png`), fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(section).toBeVisible()
  expect(await section.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath(`workflow-professional-${locale}-narrow.png`), fullPage: true })
  await page.getByRole('tab', { name: locale === 'en' ? 'Inputs' : '输入参数', exact: true }).click()
  await expect(section.locator('.query-result')).toHaveCount(0)
  await page.getByRole('tab', { name: locale === 'en' ? 'Result' : '执行结果', exact: true }).click()
  await expect(section.locator('.query-result')).toHaveCount(0)
  await read.click(); await expect(section.locator('.query-result')).toBeVisible()
  await page.evaluate(async () => { const { default: router } = await import('/src/router/index.js'); await router.push('/executions/another') })
  await expect(section.locator('.query-result')).toHaveCount(0)
  expect(errors).toEqual([])
})

test('expired or denied professional results never disclose history rows and do not rerun computation', async ({ page }) => {
  let status = 409, requests = 0, mutations = 0
  await fixture(page, 'zh-cn', route => {
    requests++
    return status === 200 ? json(route, live(ID, new Date(Date.now() - 1).toISOString())) : json(route, { error_code: 'workflow_result_unavailable' }, status)
  })
  page.on('request', request => { if (request.method() === 'POST' && new URL(request.url()).pathname.startsWith('/api/v1/develop/')) mutations++ })
  await page.goto(`/executions/${ID}`)
  const read = page.getByRole('button', { name: '查看当次明细', exact: true })
  for (const code of [409, 403, 404, 200]) {
    status = code; await read.click()
    await expect(page.locator('.workflow-professional-result')).toContainText('当次明细已过期或暂不可读取')
    await expect(page.locator('.workflow-professional-table')).toHaveCount(0)
  }
  expect(requests).toBe(4); expect(mutations).toBe(0)
})

test('deadline clears table and sum while recheck hangs, and late values stay cleared', async ({ page }) => {
  let count = 0, pending
  let deadline
  await fixture(page, 'zh-cn', async route => {
    count++
    if (count === 1) { deadline = new Date(Date.now() + 8500).toISOString(); return json(route, live(ID, deadline)) }
    pending = route
    await new Promise(resolve => { pending.finish = resolve })
    await json(route, live(ID, deadline))
  })
  await page.goto(`/executions/${ID}`)
  await page.getByRole('button', { name: '查看当次明细', exact: true }).click()
  await expect(page.locator('.workflow-professional-table')).toBeVisible()
  await page.locator('.sum-column-select').click(); await page.getByRole('option', { name: 'footprint', exact: true }).click()
  await expect.poll(() => Boolean(pending), { timeout: 8000 }).toBe(true)
  await expect(page.locator('.workflow-professional-table')).toHaveCount(0, { timeout: 6000 })
  await expect(page.getByTestId('professional-sum')).toHaveCount(0)
  pending.finish()
  await expect.poll(() => count).toBe(2)
  await expect(page.locator('.workflow-professional-table')).toHaveCount(0)
})

test('leaving the result tab discards initial late delivery and revoked rechecks clear visible rows', async ({ page }) => {
  let pending, deliver, denied = false
  await fixture(page, 'zh-cn', async route => {
    if (!deliver) {
      pending = route
      await new Promise(resolve => { deliver = resolve })
    }
    return denied ? json(route, { error_code: 'permission_denied' }, 403) : json(route, live(ID, new Date(Date.now() + 60000).toISOString()))
  })
  await page.goto(`/executions/${ID}`)
  await page.getByRole('button', { name: '查看当次明细', exact: true }).click()
  await expect.poll(() => Boolean(pending)).toBe(true)
  await page.getByRole('tab', { name: '输入参数', exact: true }).click(); deliver()
  await page.getByRole('tab', { name: '执行结果', exact: true }).click()
  await expect(page.locator('.workflow-professional-table')).toHaveCount(0)
  await page.getByRole('button', { name: '查看当次明细', exact: true }).click()
  await expect(page.locator('.workflow-professional-table')).toBeVisible()
  denied = true
  await expect(page.locator('.workflow-professional-table')).toHaveCount(0, { timeout: 8000 })
})

test('current permission changes clear visible professional details immediately', async ({ page }) => {
  await fixture(page, 'zh-cn', route => json(route, live(ID, new Date(Date.now() + 60000).toISOString())))
  await page.goto(`/executions/${ID}`)
  await page.getByRole('button', { name: '查看当次明细', exact: true }).click()
  await expect(page.locator('.workflow-professional-table')).toBeVisible()
  await page.evaluate(async () => {
    const { useAuthStore } = await import('/src/store/auth.js')
    const auth = useAuthStore()
    const context = JSON.parse(JSON.stringify(auth.authContext))
    for (const role of context.authorization.role_assignments) {
      role.permissions = role.permissions.filter(permission => permission !== 'develop.data_read.execute')
    }
    auth.authContext = context
  })
  await expect(page.locator('.workflow-professional-table')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '查看当次明细', exact: true })).toHaveCount(0)
})
