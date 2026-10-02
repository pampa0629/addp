import { expect, test } from '@playwright/test'

async function installBackend(page, { empty = false, unavailable = false, eventsDenied = false, waiting = false, executionStatus = 'failed', locale = 'zh-cn', children = [] } = {}) {
  const execution = { id: 1, execution_id: 'diagnostic-run', module: 'orchestrator', task_type: 'orchestration', source: 'orchestrator', source_task_id: 'deleted-task', source_task_name: '历史任务', status: 'failed', progress: 50, current_step: 'transform', metadata: {}, error_details: { code: 'STEP_FAILED', category: 'connection_failed' }, steps: waiting ? [{ id: 'child', status: 'running', phase: 'waiting' }, { id: 'submit', status: 'running', phase: 'dispatching' }, { id: 'uncertain', status: 'failed', error_code: 'submission_uncertain' }] : empty ? [] : [{ id: 'read', status: 'success', started_at: '2026-10-01T01:00:00Z', duration: 120 }, { id: 'transform', status: 'failed', duration: 8, error_code: 'connection_failed' }], steps_truncated: false }
  execution.status = executionStatus
  let treeRequests = 0
  await page.addInitScript(lang => localStorage.setItem('addp-lang', lang), locale)
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname
    let body
    let status = 200
    if (path === '/api/v1/system/refresh') body = { access_token: 'fixture-token', expires_in: 3600 }
    else if (path === '/api/v1/system/users/me') body = { id: 9, username: 'fixture-user' }
    else if (path === '/api/v1/system/auth/context') body = { context: { type: 'tenant', tenant_id: '7' }, authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '7' }, permissions: ['monitor.execution.read'] }] } }
    else if (path === '/api/v1/monitor/task-providers') body = []
    else if (path.endsWith('/tree')) { treeRequests++; body = { execution, children, truncated: !empty }; if (unavailable) { status = 503; body = { error_code: 'execution_owner_unavailable', error: '任务所属模块暂时无法完成读取授权' } } }
    else if (path.endsWith('/events')) {
      if (eventsDenied) { status = 403; body = { error_code: 'permission_denied' } }
      else { const after = new URL(route.request().url()).searchParams.get('after'); body = { items: empty ? [] : [{ id: after === '0' ? 1 : 2, execution_id: path.split('/').at(-2), attempt: 1, occurred_at: new Date().toISOString(), kind: after === '0' ? 'progress' : 'truncated', counters: after === '0' ? { records_written: 123 } : {} }], next_cursor: after === '0' ? 1 : 2, has_more: !empty && after === '0', retained_after: new Date(Date.now() - 30 * 86400000).toISOString() } }
    }
    else if (path === '/api/v1/monitor/executions') body = { executions: [execution], total: 1, page: 1, page_size: 20 }
    else throw new Error(`Unexpected fixture API ${path}`)
    await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
  })
  return { getTreeRequests: () => treeRequests }
}

test('shows recorded steps, sanitized failure and truncation without raw results', async ({ page }) => {
  await installBackend(page)
  await page.goto('/executions?execution_id=diagnostic-run')
  const steps = page.locator('.execution-steps')
  await expect(steps.getByRole('row')).toHaveCount(3)
  await expect(steps.getByText('read', { exact: true })).toBeVisible()
  await expect(steps.getByText('连接失败', { exact: true })).toBeVisible()
  await expect(steps.getByRole('status')).toBeVisible()
  await expect(page.locator('.metadata-raw-collapse, .workflow-result-json')).toHaveCount(0)
  await page.setViewportSize({ width: 620, height: 700 })
  await expect(steps).toBeVisible()
  expect(await steps.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBeTruthy()
})

test('states missing history without inventing steps', async ({ page }) => {
  await installBackend(page, { empty: true })
  await page.goto('/executions?execution_id=diagnostic-run')
  await expect(page.locator('.execution-steps-empty')).toBeVisible()
  await expect(page.locator('.execution-steps tbody tr')).toHaveCount(0)
})

test('shows a load failure when the owner cannot authorize details', async ({ page }) => {
  await installBackend(page, { unavailable: true })
  await page.goto('/executions?execution_id=diagnostic-run')
  await expect(page.getByRole('button', { name: '重新加载', exact: true })).toBeVisible()
  await expect(page.locator('.execution-steps')).toHaveCount(0)
})

test('pages structured events and clears evidence after permission revocation', async ({ page }) => {
  await installBackend(page)
  await page.goto('/executions?execution_id=diagnostic-run')
  const events = page.locator('.execution-events')
  await expect(events.getByText('123', { exact: false })).toBeVisible()
  await events.getByRole('button', { name: '加载后续事件', exact: true }).click()
  await expect(events.getByRole('status')).toBeVisible()
  await page.route('**/api/v1/monitor/**/events*', route => route.fulfill({ status: 403, contentType: 'application/json', body: JSON.stringify({ error_code: 'permission_denied' }) }))
  await events.getByRole('button', { name: '刷新过程', exact: true }).click()
  await expect(events.getByRole('alert')).toBeVisible()
  await expect(events.locator('tbody tr')).toHaveCount(0)
})


test('distinguishes child submission, waiting and uncertain submission', async ({ page }) => {
  await installBackend(page, { waiting: true, executionStatus: 'running' })
  await page.goto('/executions?execution_id=diagnostic-run')
  const steps = page.locator('.execution-steps')
  await expect(steps.getByText('等待下游执行', { exact: true })).toBeVisible()
  await expect(steps.getByText('正在提交下游任务', { exact: true })).toBeVisible()
  await expect(steps.getByText('提交结果不确定，未自动重放；请核对子执行', { exact: true })).toBeVisible()
})

for (const executionStatus of ['failed', 'timeout', 'cancelled']) {
  test(`${executionStatus} parent labels unfinished step evidence as last recorded`, async ({ page }) => {
    await installBackend(page, { waiting: true, executionStatus })
    await page.goto('/executions?execution_id=diagnostic-run')
    const steps = page.locator('.execution-steps')
    await expect(steps.getByText('最后记录：运行中', { exact: true })).toHaveCount(2)
    await expect(steps.getByText('最后记录：等待下游执行', { exact: true })).toBeVisible()
    await expect(steps.getByText('最后记录：正在提交下游任务', { exact: true })).toBeVisible()
    await expect(steps.locator('.execution-step-observation-ended')).toBeVisible()
    await expect(steps.locator('tr[data-status="running"]')).toHaveCount(2)
  })
}

test('last recorded observations use the selected English locale', async ({ page }) => {
  await installBackend(page, { waiting: true, locale: 'en' })
  await page.goto('/executions?execution_id=diagnostic-run')
  const steps = page.locator('.execution-steps')
  await expect(steps.getByText('Last recorded: Running', { exact: true })).toHaveCount(2)
  await expect(steps.getByText('Last recorded: Waiting for the child execution', { exact: true })).toBeVisible()
  await expect(steps.locator('.execution-step-observation-ended')).toContainText('open the child execution')
  await page.setViewportSize({ width: 620, height: 700 })
  expect(await steps.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBeTruthy()
})

test('a failed parent keeps refreshing a running visible child until its own terminal state', async ({ page }) => {
  const child = { execution: { id: 2, execution_id: 'child-run', parent_execution_id: 'diagnostic-run', module: 'meta', task_type: 'scan', source_task_name: '独立子任务', status: 'running', progress: 0, steps: [], metadata: {} }, children: [], truncated: false }
  const backend = await installBackend(page, { waiting: true, children: [child] })
  await page.goto('/executions?execution_id=diagnostic-run')
  await page.locator('.el-tree-node__content').filter({ hasText: '独立子任务' }).click()
  await expect(page.locator('.execution-detail-content')).toContainText('child-run')
  child.execution.status = 'success'
  child.execution.progress = 100
  await expect(page.locator('.execution-detail-content .el-descriptions').first()).toContainText('成功')
  await expect(page.locator('.execution-detail-content')).toContainText('child-run')
  const terminalRequests = backend.getTreeRequests()
  await page.waitForTimeout(1400)
  expect(backend.getTreeRequests()).toBe(terminalRequests)
})

for (const status of [403, 404]) {
  test(`tree authorization ${status} clears previously displayed steps and events`, async ({ page }) => {
    await installBackend(page, { executionStatus: 'running' })
    await page.goto('/executions?execution_id=diagnostic-run')
    await expect(page.locator('.execution-steps tbody tr')).toHaveCount(2)
    await page.route('**/api/v1/monitor/**/tree', route => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ error: 'not found' }) }))
    await expect(page.getByRole('button', { name: '重新加载', exact: true })).toBeVisible()
    await expect(page.locator('.execution-steps, .execution-events, .el-tree')).toHaveCount(0)
  })
}

test('a delayed denial from a closed detail cannot clear a newly authorized reopening', async ({ page }) => {
  await installBackend(page, { executionStatus: 'running' })
  await page.goto('/executions?execution_id=diagnostic-run')
  await expect(page.locator('.execution-steps tbody tr')).toHaveCount(2)
  let release, completed, blocked = false
  const pending = new Promise(resolve => { release = resolve })
  const finished = new Promise(resolve => { completed = resolve })
  await page.route('**/api/v1/monitor/**/tree', async route => {
    if (blocked) return route.fallback()
    blocked = true
    await pending
    await route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ error: 'not found' }) })
    completed()
  })
  try {
    await expect.poll(() => blocked).toBe(true)
    await page.locator('.execution-detail-dialog .el-dialog__headerbtn').click()
    await expect(page.getByRole('dialog')).not.toBeVisible()
    await page.locator('.execution-list').evaluate(el => { void el.__vueParentComponent.proxy.$router.push({ path: '/executions', query: { execution_id: 'diagnostic-run' } }) })
    await expect(page.locator('.execution-steps tbody tr')).toHaveCount(2)
    release()
    await finished
    await page.waitForTimeout(300)
    await expect(page.locator('.execution-steps tbody tr')).toHaveCount(2)
    await expect(page.getByRole('button', { name: '重新加载', exact: true })).toHaveCount(0)
  } finally {
    release()
  }
})
