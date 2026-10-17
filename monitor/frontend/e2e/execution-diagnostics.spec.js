import { expect, test } from '@playwright/test'

async function installBackend(page, { empty = false, unavailable = false, eventsDenied = false, waiting = false, executionStatus = 'failed', locale = 'zh-cn', children = [], metadata = {}, queryFailure = false, targetExists = false, executionID = 'diagnostic-run' } = {}) {
  const execution = { id: 1, execution_id: 'diagnostic-run', module: 'orchestrator', task_type: 'orchestration', source: 'orchestrator', source_task_id: 'deleted-task', source_task_name: '历史任务', status: 'failed', progress: 50, current_step: 'transform', metadata: {}, error_details: { code: 'STEP_FAILED', category: 'connection_failed' }, steps: waiting ? [{ id: 'child', status: 'running', phase: 'waiting' }, { id: 'submit', status: 'running', phase: 'dispatching' }, { id: 'uncertain', status: 'failed', error_code: 'submission_uncertain' }] : empty ? [] : [{ id: 'read', status: 'success', started_at: '2026-10-01T01:00:00Z', duration: 120 }, { id: 'transform', status: 'failed', duration: 8, error_code: 'connection_failed' }], steps_truncated: false }
  execution.status = executionStatus
  execution.execution_id = executionID
  execution.metadata = metadata
  if (queryFailure) {
    Object.assign(execution, { module: 'develop', task_type: 'query', source: 'develop', source_task_id: null, source_task_name: null, steps: [], error_details: {
      code: 'table_not_found', category: 'execution_failed', diagnostic: {
        schema_version: 'addp.execution-failure/v3', stage: 'preparation', reason: 'table_not_found',
        sql: { object: { kind: 'table', name: 'public.farmla1nd' }, position: { line: 1, column: 15 } }
      }
    } })
  }

  if (targetExists) Object.assign(execution, {
    module: 'develop', task_type: 'workflow', source: 'develop', steps: [],
    error_details: { code: 'develop.workflow.target_exists', category: 'execution_failed',
      diagnostic: { schema_version: 'addp.execution-failure/v3', stage: 'result', reason: 'target_exists' } }
  })
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
      else if (queryFailure && !empty) body = { items: [{ id: 1, execution_id: executionID, attempt: 1, occurred_at: new Date().toISOString(), kind: 'failed', counters: {} }], next_cursor: 1, has_more: false, retained_after: new Date(Date.now() - 30 * 86400000).toISOString() }
      else { const after = new URL(route.request().url()).searchParams.get('after'); body = { items: empty ? [] : [{ id: after === '0' ? 1 : 2, execution_id: path.split('/').at(-2), attempt: 1, occurred_at: new Date().toISOString(), kind: after === '0' ? 'progress' : 'truncated', counters: after === '0' ? { records_written: 123 } : {} }], next_cursor: after === '0' ? 1 : 2, has_more: !empty && after === '0', retained_after: new Date(Date.now() - 30 * 86400000).toISOString() } }
    }
    else if (path === '/api/v1/monitor/executions') body = { executions: [execution], total: 1, page: 1, page_size: 20 }
    else throw new Error(`Unexpected fixture API ${path}`)
    await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
  })
  return { getTreeRequests: () => treeRequests }
}

for (const status of [403, 503]) test(`list read failure ${status} clears rows and total until an authorized retry`, async ({ page }) => {
  await installBackend(page)
  await page.goto('/executions')
  const rows = page.locator('.execution-list .el-table__row')
  const total = page.locator('.el-pagination__total')
  await expect(rows).toHaveCount(1)
  await expect(total).toContainText('1')
  await page.route('**/api/v1/monitor/executions?*', route => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ error_code: status === 503 ? 'execution_owner_unavailable' : 'permission_denied' }) }))
  await page.getByRole('button', { name: '查询', exact: true }).click()
  await expect(rows).toHaveCount(0)
  await expect(total).toContainText('0')
  await page.unroute('**/api/v1/monitor/executions?*')
  await page.getByRole('button', { name: '查询', exact: true }).click()
  await expect(rows).toHaveCount(1)
  await expect(total).toContainText('1')
})

test('a delayed list denial cannot clear a newer authorized result', async ({ page }) => {
  await installBackend(page)
  await page.goto('/executions')
  const rows = page.locator('.execution-list .el-table__row')
  await expect(rows).toHaveCount(1)
  let release, completed, blocked = false
  const pending = new Promise(resolve => { release = resolve })
  const finished = new Promise(resolve => { completed = resolve })
  await page.route('**/api/v1/monitor/executions?*', async route => {
    if (blocked) return route.fallback()
    blocked = true
    await pending
    await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error_code: 'execution_owner_unavailable' }) })
    completed()
  })
  try {
    await page.getByRole('button', { name: '查询', exact: true }).click()
    await expect.poll(() => blocked).toBe(true)
    const authorized = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/monitor/executions' && response.status() === 200)
    await page.getByRole('button', { name: '查询', exact: true }).click()
    await authorized
    await expect(rows).toHaveCount(1)
    release()
    await finished
    await page.waitForTimeout(300)
    await expect(rows).toHaveCount(1)
    await expect(page.locator('.el-pagination__total')).toContainText('1')
  } finally {
    release()
  }
})

test('a query execution deep link opens its failure details and matching process events without a task definition', async ({ page }) => {
  const executionID = '11111111-1111-4111-8111-111111111111'
  await installBackend(page, { executionID, queryFailure: true })
  const treeRequest = page.waitForRequest(request => new URL(request.url()).pathname === `/api/v1/monitor/executions/by-execution-id/${executionID}/tree`)
  const eventsRequest = page.waitForRequest(request => new URL(request.url()).pathname === `/api/v1/monitor/executions/by-execution-id/${executionID}/events`)
  await page.goto(`/executions?execution_id=${executionID}`)
  await Promise.all([treeRequest, eventsRequest])
  const diagnostic = page.locator('.execution-detail-content > .execution-failure')
  await expect(diagnostic).toContainText(executionID)
  await expect(diagnostic).toContainText('table_not_found')
  await expect(diagnostic).toBeInViewport()
  await expect(diagnostic.getByRole('button', { name: '查看监控错误详情', exact: true })).toHaveCount(0)
  await expect(page.locator('.execution-events tbody')).toContainText('执行失败')
  await expect(page.getByRole('button', { name: '打开任务定义', exact: true })).toHaveCount(0)
})

for (const locale of ['zh-cn', 'en']) test(`shows query failure at the top and opens an ad-hoc Owner execution (${locale})`, async ({ page }) => {
  await installBackend(page, { locale, queryFailure: true, empty: true })
  await page.goto('/executions?execution_id=diagnostic-run')
  const diagnostic = page.locator('.execution-detail-content > .execution-failure')
  await expect(diagnostic).toContainText(locale === 'zh-cn' ? '查询引用的数据表不存在' : 'A referenced table does not exist')
  await expect(diagnostic).toContainText('table_not_found')
  await expect(diagnostic).toContainText('diagnostic-run')
  await expect(diagnostic).toContainText(locale === 'zh-cn' ? '准备执行' : 'Prepare execution')
  await expect(diagnostic).toContainText('public.farmla1nd')
  await expect(diagnostic).toContainText(locale === 'zh-cn' ? '第 1 行，第 15 列' : 'Line 1, column 15')
  const button = diagnostic.getByRole('button', { name: locale === 'zh-cn' ? '查看查询执行详情' : 'View query execution details', exact: true })
  await expect(button).toBeVisible()
  await expect(page.getByRole('button', { name: locale === 'zh-cn' ? '打开任务定义' : 'Open task definition', exact: true })).toHaveCount(0)
  await page.setViewportSize({ width: 620, height: 700 })
  expect(await diagnostic.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBeTruthy()
  await page.route('**/develop/executions/diagnostic-run', route => route.fulfill({ contentType: 'text/html', body: '<main>Owner execution destination</main>' }))
  await button.click()
  await expect(page).toHaveURL(/\/develop\/executions\/diagnostic-run$/)
})

for (const locale of ['zh-cn', 'en']) test(`shows recorded recovery context (${locale})`, async ({ page }) => {
  const previous = 'aaaaaaaa-1111-4111-8111-111111111111'
  await installBackend(page, { locale, metadata: { recovered_from_execution_id: previous, recovery_attempt: 2 } })
  await page.goto('/executions?execution_id=diagnostic-run')
  const summary = page.locator('.metadata-summary')
  await expect(summary).toContainText(previous)
  await expect(summary).toContainText(locale === 'zh-cn' ? '恢复来源执行 UUID' : 'Recovered from execution UUID')
  await expect(summary).toContainText(locale === 'zh-cn' ? '恢复次数' : 'Recovery attempt')
  await expect(summary).toContainText('2')
})

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

for (const status of [401, 403, 404, 503]) test(`event read failure ${status} clears terminal details after paging`, async ({ page }) => {
  await installBackend(page)
  await page.goto('/executions?execution_id=diagnostic-run')
  const events = page.locator('.execution-events')
  await expect(events.getByText('123', { exact: false })).toBeVisible()
  await events.getByRole('button', { name: '加载后续事件', exact: true }).click()
  await expect(events.getByRole('status')).toBeVisible()
  await page.route('**/api/v1/monitor/**/events*', route => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ error_code: status === 503 ? 'execution_owner_unavailable' : 'permission_denied' }) }))
  await events.getByRole('button', { name: '刷新过程', exact: true }).click()
  await expect(page.locator('.execution-detail-content, .execution-tree, .execution-events, .execution-steps')).toHaveCount(0)
  if (status === 401) await expect(page.getByRole('dialog')).not.toBeVisible()
  else await expect(page.getByRole('button', { name: '重新加载', exact: true })).toBeVisible()
  if (status === 503) {
    await page.unroute('**/api/v1/monitor/**/events*')
    await page.getByRole('button', { name: '重新加载', exact: true }).click()
    await expect(page.locator('.execution-steps tbody tr')).toHaveCount(2)
    await expect(page.locator('.execution-events')).toContainText('123')
  }
})

test('an unrelated event service failure clears events without treating the detail as denied', async ({ page }) => {
  await installBackend(page)
  await page.goto('/executions?execution_id=diagnostic-run')
  const events = page.locator('.execution-events')
  await expect(events).toContainText('123')
  await page.route('**/api/v1/monitor/**/events*', route => route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error_code: 'event_store_unavailable' }) }))
  await events.getByRole('button', { name: '刷新过程', exact: true }).click()
  await expect(events.getByRole('alert')).toBeVisible()
  await expect(events.locator('tbody tr')).toHaveCount(0)
  await expect(page.locator('.execution-steps tbody tr')).toHaveCount(2)
  await expect(page.getByRole('button', { name: '重新加载', exact: true })).toHaveCount(0)
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

for (const status of [401, 403, 404, 503]) {
  test(`tree read failure ${status} clears previously displayed steps and events`, async ({ page }) => {
    await installBackend(page, { executionStatus: 'running' })
    await page.goto('/executions?execution_id=diagnostic-run')
    await expect(page.locator('.execution-steps tbody tr')).toHaveCount(2)
    await page.route('**/api/v1/monitor/**/tree', route => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ error_code: status === 503 ? 'execution_owner_unavailable' : 'permission_denied' }) }))
    await expect(page.locator('.execution-detail-content, .execution-steps, .execution-events, .el-tree')).toHaveCount(0)
    if (status === 401) await expect(page.getByRole('dialog')).not.toBeVisible()
    else await expect(page.getByRole('button', { name: '重新加载', exact: true })).toBeVisible()
    if (status === 503) {
      await page.unroute('**/api/v1/monitor/**/tree')
      await page.getByRole('button', { name: '重新加载', exact: true }).click()
      await expect(page.locator('.execution-steps tbody tr')).toHaveCount(2)
    }
  })
}

for (const status of [404, 503]) test(`a delayed event read failure ${status} cannot clear a newly authorized reopening`, async ({ page }) => {
  await installBackend(page)
  await page.goto('/executions?execution_id=diagnostic-run')
  const events = page.locator('.execution-events')
  await expect(events).toContainText('123')
  let release, completed, blocked = false
  const pending = new Promise(resolve => { release = resolve })
  const finished = new Promise(resolve => { completed = resolve })
  await page.route('**/api/v1/monitor/**/events*', async route => {
    if (blocked) return route.fallback()
    blocked = true
    await pending
    await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ error_code: status === 503 ? 'execution_owner_unavailable' : 'permission_denied' }) })
    completed()
  })
  try {
    await events.getByRole('button', { name: '刷新过程', exact: true }).click()
    await expect.poll(() => blocked).toBe(true)
    await page.locator('.execution-detail-dialog .el-dialog__headerbtn').click()
    await expect(page.getByRole('dialog')).not.toBeVisible()
    await page.locator('.execution-list').evaluate(el => { void el.__vueParentComponent.proxy.$router.push({ path: '/executions', query: { execution_id: 'diagnostic-run' } }) })
    await expect(page.locator('.execution-events')).toContainText('123')
    release()
    await finished
    await page.waitForTimeout(300)
    await expect(page.locator('.execution-steps tbody tr')).toHaveCount(2)
    await expect(page.locator('.execution-events')).toContainText('123')
    await expect(page.getByRole('button', { name: '重新加载', exact: true })).toHaveCount(0)
  } finally {
    release()
  }
})

for (const status of [404, 503]) test(`a delayed tree read failure ${status} from a closed detail cannot clear a newly authorized reopening`, async ({ page }) => {
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
    await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ error_code: status === 503 ? 'execution_owner_unavailable' : 'permission_denied' }) })
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

for (const status of [403, 404]) test(`fresh Monitor-only deep link ${status} shows denial without protected facts`, async ({ page }) => {
  await installBackend(page, { empty: true })
  await page.route('**/api/v1/monitor/**/tree', route => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ error: 'not found' }) }))
  await page.route('**/api/v1/monitor/executions*', route => route.fulfill({ json: { executions: [], total: 0, page: 1, page_size: 20 } }))
  await page.goto('/executions?execution_id=diagnostic-run')
  const title = page.locator('.execution-detail-dialog .el-result__title')
  await expect(title).toBeVisible()
  await expect(title).toHaveText('未能加载目标执行详情')
  await expect(page.getByRole('dialog').getByRole('status')).toHaveText('未能加载目标执行详情')
  await expect(page.locator('.execution-detail-content, .execution-steps, .execution-events, .el-tree')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '重新加载', exact: true })).toBeVisible()
})


for (const locale of ['zh-cn', 'en']) test(`shows actionable create conflict in workflow execution (${locale})`, async ({ page }) => {
  await installBackend(page, { locale, targetExists: true, empty: true })
  await page.goto('/executions?execution_id=diagnostic-run')
  const diagnostic = page.locator('.execution-detail-content > .execution-failure')
  await expect(diagnostic).toContainText(locale === 'zh-cn'
    ? '输出目标已存在，create 模式已拒绝覆盖。'
    : 'The output target already exists; create mode refused to overwrite it.')
  await expect(diagnostic).toContainText(locale === 'zh-cn'
    ? '请更换输出文件名或输出位置后重新执行。'
    : 'Choose another output file name or location, then run again.')
  await expect(diagnostic).toContainText('develop.workflow.target_exists')
  await page.setViewportSize({ width: 620, height: 700 })
  expect(await diagnostic.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBeTruthy()
})
