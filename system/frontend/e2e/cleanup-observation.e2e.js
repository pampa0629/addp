import { browserTestOrigin } from '../../../common-frontend/basic/src/utils/browserTestIsolation.mjs'
import { expect, test } from '@playwright/test'
const requestId = 'a8eb1e50-fbe5-4c40-b096-bf04a64eb141', childId = 'bf0044f5-3bc7-48a0-b14a-c4f6a44d8ce2'
const at = '2026-10-29T01:00:00Z', digest = 'a'.repeat(64)
const task = { task_id: requestId, execution_id: requestId, action: 'scan', status: 'timeout', expected_modules: ['graph'], requested_by: 1, trigger_type: 'manual', context: {}, created_at: at, started_at: at, completed_at: at }
const child = { execution_id: childId, parent_execution_id: requestId, module: 'graph', task_type: 'cleanup_executor', source: 'system', status: 'pending', attempt: 0, progress: 0,
  cleanup_admission: { schema: 'addp.cleanup-admission-view/v1', state: 'closed_unclaimed', basis: 'parent_terminal', observed_at: at } }
function data(rows = [child]) { return { task_id: requestId, task: { ...task }, action: 'scan', status: 'timeout', observed_at: at, cutoff_at: at, module_executions: rows } }
async function fixture(page, initial = data()) {
  const state = { data: initial, detailStatus: 200, delayProfessional: null, professionalReads: 0, writes: [], history: [] }
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  await page.route('**/api/v1/**', async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname
    const headers = { 'access-control-allow-origin': req.headers().origin || browserTestOrigin('system'), 'access-control-allow-credentials': 'true', 'access-control-allow-headers': 'authorization,content-type', 'access-control-allow-methods': 'GET,POST,OPTIONS' }
    const reply = (json, status = 200) => route.fulfill({ json, status, headers })
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers })
    if (path.endsWith('/refresh')) return reply({ access_token: 'cleanup-fixture', expires_in: 3600 })
    if (path.endsWith('/users/me')) return reply({ id: '1', display_name: 'Cleanup Reader' })
    if (path.endsWith('/auth/context')) return reply({ principal: { id: '1', principal_type: 'user' }, context: { type: 'tenant', tenant_id: '2', tenant_membership_id: '4' }, authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '2' }, permissions: ['system.cleanup.read', 'system.cleanup.execute', 'graph.cleanup_result.read'] }] } })
    if (path.endsWith('/admin/cleanup/history')) {
      state.history.push(Object.fromEntries(url.searchParams))
      return reply({ tasks: [state.data.task], total: 21, page: Number(url.searchParams.get('page')), page_size: 10 })
    }
    if (path.includes('/admin/cleanup/tasks/')) return reply(state.detailStatus === 200 ? state.data : { error: 'owner unavailable', error_code: 'execution_owner_unavailable' }, state.detailStatus)
    if (path.endsWith('/admin/cleanup/scan')) { state.writes.push(req.postDataJSON()); return reply({ error: 'temporarily unavailable' }, 503) }
    if (path === `/api/v1/graph/cleanup-executions/${childId}/result`) {
      state.professionalReads++
      if (state.delayProfessional) await state.delayProfessional
      return reply({ schema: 'addp.cleanup-professional-result/v1', execution_id: childId, request_id: requestId, module: 'graph', attempt: 1, action: 'scan', status: 'success', summary: {}, statistics: { graphs: 987654 } })
    }
    throw new Error(`Unexpected Cleanup fixture request ${req.method()} ${path}`)
  })
  return state
}
test('closed coordinator preserves pending, absent start and absent result; history uses actual pages', async ({ page }) => {
  const errors = []; page.on('pageerror', error => errors.push(error.message))
  const state = await fixture(page)
  await page.goto('/cleanup')
  await expect(page.getByText('协调已关闭，模块未开始', { exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'graph · 待领取' })).toBeVisible()
  await expect(page.locator('.cleanup-module-results dd').nth(1)).toHaveText('—')
  await expect(page.getByText('原始 JSON', { exact: true })).toHaveCount(0)
  await expect(page.locator('.result-banner')).toContainText('超时')
  expect(state.professionalReads).toBe(0)
  expect(state.history[0]).toEqual({ page: '1', page_size: '10' })
  await page.locator('.el-pagination .btn-next').click()
  await expect.poll(() => state.history.at(-1)?.page).toBe('2')
  expect(errors).toEqual([])
})
test('failed Owner read or invalid later evidence clears the old admission', async ({ page }) => {
  const state = await fixture(page)
  await page.goto('/cleanup')
  await expect(page.getByText('协调已关闭，模块未开始', { exact: true })).toBeVisible()
  state.detailStatus = 503
  await page.getByRole('button', { name: '刷新历史', exact: true }).click()
  await expect(page.getByText('协调已关闭，模块未开始', { exact: true })).toHaveCount(0)
  state.detailStatus = 200; state.data = data([{ ...child, attempt: 1 }])
  await page.getByRole('button', { name: '刷新历史', exact: true }).click()
  await expect(page.getByText('协调已关闭，模块未开始', { exact: true })).toHaveCount(0)
  expect(state.professionalReads).toBe(0)
})
test('professional fetch is lazy and late data cannot survive parent invalidation', async ({ page }) => {
  const receipt = { schema: 'addp.cleanup-receipt/v1', execution_id: childId, parent_execution_id: requestId, request_id: requestId, request_digest: digest, module: 'graph', attempt: 1, action: 'scan', status: 'success', summary: {}, impact: {}, impact_digest: digest }
  const state = await fixture(page, data([{ ...child, status: 'success', attempt: 1, cleanup_admission: undefined, started_at: at, receipt }]))
  let release; state.delayProfessional = new Promise(resolve => { release = resolve })
  await page.goto('/cleanup')
  await expect(page.getByRole('heading', { name: 'graph · 成功' })).toBeVisible()
  expect(state.professionalReads).toBe(0)
  await page.getByRole('button', { name: '模块专业结果', exact: true }).click()
  await expect.poll(() => state.professionalReads).toBe(1)
  state.detailStatus = 503
  await page.getByRole('button', { name: '刷新历史', exact: true }).click()
  await expect(page.locator('.cleanup-module-results')).toHaveCount(0)
  release()
  await expect(page.getByText('987654', { exact: true })).toHaveCount(0)
})
test('unknown scan acceptance retries the exact original request UUID', async ({ page }) => {
  const state = await fixture(page)
  await page.goto('/cleanup')
  const create = page.getByRole('button', { name: '评估待回收资源', exact: true })
  await create.click(); await expect.poll(() => state.writes.length).toBe(1)
  await expect(create).toBeEnabled(); await create.click()
  await expect.poll(() => state.writes.length).toBe(2)
  expect(state.writes[0]).toEqual(state.writes[1])
  expect(state.writes[0].request_id).toMatch(/^[a-f0-9-]{36}$/)
})

test('an identity change clears professional results and rejects the old response', async ({ page }) => {
  const receipt = { schema: 'addp.cleanup-receipt/v1', execution_id: childId, parent_execution_id: requestId, request_id: requestId, request_digest: digest, module: 'graph', attempt: 1, action: 'scan', status: 'success', summary: {}, impact: {}, impact_digest: digest }
  const state = await fixture(page, data([{ ...child, status: 'success', attempt: 1, cleanup_admission: undefined, started_at: at, receipt }]))
  let release
  state.delayProfessional = new Promise(resolve => { release = resolve })
  try {
    await page.goto('/cleanup')
    await page.getByRole('button', { name: '模块专业结果', exact: true }).click()
    await expect.poll(() => state.professionalReads).toBe(1)
    await page.evaluate(async () => {
      const store = (await import('/src/store/auth.js')).useAuthStore()
      const context = JSON.parse(JSON.stringify(store.authContext))
      context.principal.id = 'other-user'
      context.context.tenant_id = 'another-tenant'
      context.authorization.role_assignments = []
      store.authContext = context
    })
    await expect(page.locator('.cleanup-module-results')).toHaveCount(0)
    release()
    await expect(page.getByText('987654', { exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '模块专业结果', exact: true })).toHaveCount(0)
  } finally {
    release()
  }
})
