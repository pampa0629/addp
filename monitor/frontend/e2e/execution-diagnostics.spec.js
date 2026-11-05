import { expect, test } from '@playwright/test'

async function installBackend(page, { empty = false, unavailable = false, eventsDenied = false, waiting = false, executionStatus = 'failed', locale = 'zh-cn', children = [], metadata = {}, queryFailure = false, errorDetails, targetExists = false, executionID = 'diagnostic-run', cleanup = false } = {}) {
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
  if (errorDetails) execution.error_details = errorDetails
  if (cleanup) Object.assign(execution, {
    module: 'graph', task_type: 'cleanup_executor', source: 'system', source_task_id: null,
    status: 'pending', attempt: 0, progress: 0, started_at: null, error_details: null, steps: [],
    cleanup_admission: { schema: 'addp.cleanup-admission-view/v1', state: 'closed_unclaimed', basis: 'parent_terminal', observed_at: '2026-10-29T01:00:00Z' }
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

test('Cleanup admission preserves the real pending state and clears on authorization loss', async ({ page }) => {
  await installBackend(page, { cleanup: true, empty: true })
  await page.goto('/executions?execution_id=diagnostic-run')
  const admission = page.locator('.execution-detail-content > .cleanup-admission')
  await expect(admission).toContainText('协调已关闭，模块未开始')
  await expect(page.locator('.execution-tree-node-main .el-tag').last()).toHaveText('等待中')
  await page.route('**/api/v1/monitor/**/events*', route => route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error_code: 'execution_owner_unavailable' }) }))
  await page.locator('.execution-events').getByRole('button', { name: '刷新过程', exact: true }).click()
  await expect(admission).toHaveCount(0)
  await expect(page.getByRole('button', { name: '重新加载', exact: true })).toBeVisible()
})

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
  await expect(diagnostic).toContainText(locale === 'zh-cn' ? '查询引用的表或视图不存在' : 'A referenced table or view does not exist')
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

for (const locale of ['zh-cn', 'en']) for (const [reason, stage, zh, en, zhAction, enAction] of [
  ['protection_role_choice_unavailable', 'preparation', '所选脱敏方式或当前角色资格已不可用', 'selected masking method or current role eligibility is unavailable', '重新选择并执行查询', 'choose again and rerun'],
  ['data_protection_required', 'preparation', '数据脱敏保护校验未通过', 'data masking protection checks'],
  ['permission_denied', 'execution', '当前执行缺少读取或写入权限', 'lacks read or write permission'],
  ['authorization_invalid', 'authorization', '执行授权缺失、失效或与本次执行不匹配', 'Execution authorization is missing, expired'],
  ['protection_computed_output_unsupported', 'preparation', '当前计算或组合输出涉及受保护字段', 'computed or combined output uses protected fields', '请改为直接读取受保护字段', 'Read the protected fields directly'],
  ['protection_lineage_unverified', 'preparation', '完整读取范围或输出字段来源', 'complete read scope or output field origins', '能验证数据来源', 'verifiable data origins'],
  ['protection_not_ready', 'preparation', '数据保护配置尚未就绪', 'Data protection configuration is not ready', '纳管和规则同步完成', 'enrollment and rule synchronization'],
  ['protection_expired', 'result', '保护规则已到期', 'protection rules for this data resource have expired', '续期或设置长期有效', 'renew data protection or set permanent validity'],
  ['protection_not_yet_valid', 'preparation', '保护规则尚未生效', 'protection rules for this data resource are not yet effective', '等待保护规则的生效时间', 'Wait until the protection rules take effect'],
  ['protection_schema_mismatch', 'preparation', '数据结构与保护规则不匹配', 'data structure does not match its protection rules', '更新元数据并重新确认保护规则', 'update metadata and reconfirm the protection rules'],
  ['protection_operation_unsupported', 'preparation', '当前操作暂不支持所需的数据脱敏保护', 'operation does not yet support the required data masking protection', '支持该保护规则的执行方式', 'execution method that supports these protection rules']
]) test(`distinguishes protection from access permission and execution authorization (${locale}, ${reason})`, async ({ page }) => {
  const protection = reason === 'data_protection_required' || reason.startsWith('protection_')
  await installBackend(page, { locale, queryFailure: true, empty: true, errorDetails: {
    category: protection ? 'permission_denied' : 'execution_failed',
    code: protection ? 'develop.query.protection_required' : reason,
    message: 'private-runtime-value SELECT phone',
    diagnostic: { schema_version: 'addp.execution-failure/v3', stage, reason }
  } })
  await page.goto('/executions?execution_id=diagnostic-run')
  const panel = page.locator('.execution-detail-content > .execution-failure')
  await expect(panel.locator('h4')).toContainText(locale === 'zh-cn' ? zh : en)
  await expect(panel).not.toContainText(/private-runtime-value|SELECT phone/)
  if (zhAction) await expect(panel).toContainText(locale === 'zh-cn' ? zhAction : enAction)
  if (reason === 'data_protection_required') {
    await expect(panel).toContainText(locale === 'zh-cn' ? '请联系安全管理员检查脱敏保护规则' : 'Ask a security administrator to check masking rules')
    await expect(panel).not.toContainText(/访问资格|access eligibility/i)
  } else if (!protection) {
    await expect(panel).not.toContainText(/数据脱敏保护校验|data masking protection checks/)
  }
  if (protection) await expect(panel).not.toContainText(/访问资格|access eligibility/i)
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(panel).toBeVisible()
  expect(await panel.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBeTruthy()
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


for (const locale of ['zh-cn','en']) for (const [operator_id,reason,field,parameter='input_gdf'] of [
  ...['envelope','representative_point','centroid','convex_hull','minimum_rotated_rectangle'].flatMap(operator_id=>[['operator_input_invalid','input_gdf'],['operator_crs_invalid','']].map(([reason,parameter])=>[operator_id,reason,'',parameter])),
  ...[['operator_input_invalid','input_gdf'],['operator_crs_invalid',''],['operator_nonnegative_number_required','tolerance'],['operator_boolean_required','preserve_topology']].map(([reason,parameter])=>['simplify',reason,'',parameter]),
  ...[['operator_input_invalid','input_gdf'],['operator_crs_invalid',''],['operator_number_invalid','distance'],['operator_positive_integer_required','resolution']].map(([reason,parameter])=>['buffer',reason,'',parameter]),
  ['batch_buffer','operator_crs_invalid','',''],
  ...[['operator_input_invalid','input_gdf'],['operator_mask_invalid','mask_gdf'],['operator_crs_invalid','']].map(([reason,parameter])=>['voronoi',reason,'',parameter]),
  ['batch_buffer','operator_field_conflict','distance'],
  ...['source_id','part_no','ring_no','ring_id','is_hole'].map(field=>['extract_polygon_rings','operator_field_conflict',field]),
  ['extract_polygon_rings','operator_geometry_invalid','']
]) test(`workflow operator diagnostic uses the shared panel in Monitor (${operator_id}, ${reason}, ${field}, ${locale})`,async({page})=>{
  await page.addInitScript(()=>{window.copiedDiagnostic='';Object.defineProperty(navigator,'clipboard',{value:{writeText:async text=>{window.copiedDiagnostic=text}},configurable:true})})
  await installBackend(page,{locale,empty:true,targetExists:true,errorDetails:{category:'execution_failed',code:'develop.workflow.invalid_params',message:['envelope','representative_point','centroid','convex_hull','minimum_rotated_rectangle'].includes(operator_id)?'private-password /private/input.gpkg PRIVATE_BIZ_FIELD':'private-password',
    diagnostic:{schema_version:'addp.execution-failure/v3',stage:'execution',reason,
      operator:{node_index:2,operator_id,...(parameter?{parameter}:{}),...(field?{field}:{})}}}})
  await page.goto('/executions?execution_id=diagnostic-run')
  const panel=page.locator('.execution-detail-content > .execution-failure')
  const construction=['envelope','representative_point','centroid','convex_hull','minimum_rotated_rectangle'].includes(operator_id)
  const constructionMarker=reason==='operator_input_invalid'?(locale==='zh-cn'?'几何':'geometry'):(locale==='zh-cn'?'投影坐标系':'projected CRS')
  const simplifyMarker={operator_input_invalid:locale==='zh-cn'?'几何':'geometry',operator_crs_invalid:locale==='zh-cn'?'投影坐标系':'projected CRS',operator_nonnegative_number_required:locale==='zh-cn'?'非负':'non-negative',operator_boolean_required:locale==='zh-cn'?'布尔值':'boolean'}[reason]
  const name=operator_id==='envelope'?(locale==='zh-cn'?'外包矩形':'Axis-aligned envelope'):operator_id==='representative_point'?(locale==='zh-cn'?'面内点':'Interior point'):operator_id==='centroid'?(locale==='zh-cn'?'计算质心':'Calculate centroid'):operator_id==='convex_hull'?(locale==='zh-cn'?'凸包':'Convex hull'):operator_id==='minimum_rotated_rectangle'?(locale==='zh-cn'?'最小外接矩形':'Minimum-area bounding rectangle'):operator_id==='simplify'?(locale==='zh-cn'?'简化几何':'Simplify geometry'):operator_id==='buffer'?(locale==='zh-cn'?'缓冲区分析':'Buffer analysis'):operator_id==='voronoi'?(locale==='zh-cn'?'泰森多边形':'Voronoi polygons'):operator_id==='batch_buffer'?(locale==='zh-cn'?'多距缓冲':'Multi-distance buffer'):(locale==='zh-cn'?'面界提取':'Extract polygon rings')
  await expect(panel).toContainText(name)
  if(field) await expect(panel).toContainText(field)
  else if(construction) await expect(panel).toContainText(constructionMarker)
  else if(operator_id==='simplify') await expect(panel).toContainText(simplifyMarker)
  else if(operator_id==='voronoi') await expect(panel).toContainText(reason==='operator_input_invalid'?(locale==='zh-cn'?'重复 XY':'duplicate XY'):reason==='operator_mask_invalid'?'Polygon':locale==='zh-cn'?'投影坐标系':'projected CRS')
  else if(operator_id==='buffer'||operator_id==='batch_buffer'&&reason==='operator_crs_invalid') await expect(panel).toContainText(reason==='operator_number_invalid'?(locale==='zh-cn'?'正数、零或负数':'positive, zero or negative'):reason==='operator_positive_integer_required'?(locale==='zh-cn'?'大于零的整数':'positive integer'):reason==='operator_crs_invalid'?(locale==='zh-cn'?'投影坐标系':'projected CRS'):locale==='zh-cn'?'几何':'geometry')
  else {
    await expect(panel).toContainText(locale==='zh-cn'?'未通过有效性检查':'failed the validity check')
    await expect(panel).toContainText(locale==='zh-cn'?'显式修复':'explicitly repair')
  }
  await expect(panel).toContainText(locale==='zh-cn'?'定义中的第 2 个节点':'Node 2 in the workflow definition')
  await expect(panel).toContainText('develop.workflow.invalid_params')
  await expect(panel).not.toContainText('private-password')
  await panel.getByRole('button',{name:locale==='zh-cn'?'复制诊断信息':'Copy diagnostics',exact:true}).click()
  const copied=await page.evaluate(()=>window.copiedDiagnostic)
  expect(copied).toContain(name)
  expect(copied).toContain('diagnostic-run')
  if(field) expect(copied).toContain(field)
  if(construction) {
    expect(copied).toContain(constructionMarker)
    if(parameter) { await expect(panel).toContainText(parameter); expect(copied).toContain(parameter) }
    if(reason==='operator_input_invalid') {
      await expect(panel).toContainText(locale==='zh-cn'?'唯一活动几何列':'one active geometry column')
      expect(copied).toContain('Z/M')
      if(operator_id==='representative_point') {
        await expect(panel).toContainText('Polygon')
        await expect(panel).toContainText('MultiPolygon')
        expect(copied).toContain('Polygon')
        expect(copied).toContain('MultiPolygon')
      }
    } else {
      await expect(panel).toContainText(locale==='zh-cn'?'线性单位':'horizontal linear units')
      expect(copied).toContain(locale==='zh-cn'?'不会自动投影':'no automatic projection')
    }
    await expect(panel).not.toContainText('/private/input.gpkg')
    await expect(panel).not.toContainText('PRIVATE_BIZ_FIELD')
    expect(copied).not.toMatch(/两个图层|Both layers|标识|identifier|buffer|缓冲|tolerance|容差|PRIVATE_BIZ_FIELD|\/private\/input\.gpkg/i)
    await page.setViewportSize({width:390,height:844})
    expect(await panel.evaluate(el=>el.scrollWidth<=el.clientWidth+1)).toBeTruthy()
  }
  if(operator_id==='simplify') {
    expect(copied).toContain(simplifyMarker)
    if(parameter) { await expect(panel).toContainText(parameter); expect(copied).toContain(parameter) }
    if(reason==='operator_nonnegative_number_required') {
      await expect(panel).toContainText(locale==='zh-cn'?'零':'zero')
      expect(copied).toContain(locale==='zh-cn'?'零':'zero')
    }
    if(reason==='operator_boolean_required') {
      await expect(panel).toContainText(locale==='zh-cn'?'保留单几何拓扑':'Preserve single-geometry topology')
      expect(copied).toContain('true');expect(copied).toContain('false')
    }
    expect(copied).not.toMatch(/两个图层|Both layers|标识|identifier|buffer|缓冲/i)
    await page.setViewportSize({width:390,height:844})
    expect(await panel.evaluate(el=>el.scrollWidth<=el.clientWidth+1)).toBeTruthy()
  }
  if(operator_id==='buffer'||operator_id==='batch_buffer'&&reason==='operator_crs_invalid') {
    if(parameter) { await expect(panel).toContainText(parameter); expect(copied).toContain(parameter) }
    expect(copied).toContain(reason==='operator_number_invalid'?(locale==='zh-cn'?'正数、零或负数':'positive, zero or negative'):reason==='operator_positive_integer_required'?(locale==='zh-cn'?'大于零的整数':'positive integer'):reason==='operator_crs_invalid'?(locale==='zh-cn'?'投影坐标系':'projected CRS'):locale==='zh-cn'?'几何':'geometry')
    if(reason==='operator_crs_invalid') {
      await expect(panel).not.toContainText('两个图层')
      await expect(panel).not.toContainText('Both layers')
      expect(copied).not.toMatch(/两个图层|Both layers/)
    }
    await page.setViewportSize({width:390,height:844})
    expect(await panel.evaluate(el=>el.scrollWidth<=el.clientWidth+1)).toBeTruthy()
  }
  expect(copied).not.toContain('private-password')
})

for (const locale of ['zh-cn','en']) for (const [reason,parameter,zh,en] of [
  ['operator_distance_limit_invalid','max_distance','有限正数','finite positive'],
  ['operator_source_topology_invalid','input_gdf','内部自交','internal self-intersections'],
  ['operator_trim_conflict','','交错或相遇','cross or meet']
]) test(`line trimming controlled diagnosis in Monitor (${reason}, ${locale})`,async({page})=>{
  await page.addInitScript(()=>{window.copiedDiagnostic='';Object.defineProperty(navigator,'clipboard',{value:{writeText:async text=>{window.copiedDiagnostic=text}},configurable:true})})
  await installBackend(page,{locale,empty:true,targetExists:true,errorDetails:{category:'execution_failed',code:'develop.workflow.invalid_params',message:'PRIVATE GEOS /input.gpkg',
    diagnostic:{schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:2,operator_id:'trim_line_endpoints',...(parameter?{parameter}:{})}}}})
  await page.goto('/executions?execution_id=diagnostic-run')
  const panel=page.locator('.execution-detail-content > .execution-failure')
  await expect(panel).toContainText(locale==='zh-cn'?'线端修剪':'Line endpoint trimming')
  await expect(panel).toContainText(locale==='zh-cn'?zh:en)
  await expect(panel).not.toContainText('PRIVATE')
  await panel.getByRole('button',{name:locale==='zh-cn'?'复制诊断信息':'Copy diagnostics',exact:true}).click()
  expect(await page.evaluate(()=>window.copiedDiagnostic)).toContain(locale==='zh-cn'?zh:en)
  await page.setViewportSize({width:620,height:700})
  expect(await panel.evaluate(el=>el.scrollWidth<=el.clientWidth+1)).toBeTruthy()
})


for (const locale of ['zh-cn','en']) for (const field of ['', 'node_id']) {
  test(`node extraction controlled diagnosis in Monitor (${field || 'network'}, ${locale})`,async({page})=>{
    await page.addInitScript(()=>{window.copiedDiagnostic='';Object.defineProperty(navigator,'clipboard',{value:{writeText:async text=>{window.copiedDiagnostic=text}},configurable:true})})
    const reason=field?'operator_field_conflict':'operator_endpoint_network_invalid'
    await installBackend(page,{locale,empty:true,targetExists:true,errorDetails:{category:'execution_failed',code:'develop.workflow.invalid_params',message:'PRIVATE GEOS /source/path',
      diagnostic:{schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:2,operator_id:'extract_line_nodes',parameter:'input_gdf',...(field?{field}:{})}}}})
    await page.goto('/executions?execution_id=diagnostic-run')
    const panel=page.locator('.execution-detail-content > .execution-failure')
    await expect(panel).toContainText(locale==='zh-cn'?'节点提取':'Extract line nodes')
    await expect(panel).toContainText(field || (locale==='zh-cn'?'双方端点':'both endpoints'))
    await expect(panel).not.toContainText('PRIVATE')
    await panel.getByRole('button',{name:locale==='zh-cn'?'复制诊断信息':'Copy diagnostics',exact:true}).click()
    const copied=await page.evaluate(()=>window.copiedDiagnostic)
    expect(copied).toContain('diagnostic-run');expect(copied).not.toContain('PRIVATE')
    await page.setViewportSize({width:620,height:700})
    expect(await panel.evaluate(el=>el.scrollWidth<=el.clientWidth+1)).toBeTruthy()
  })
}


for(const locale of ['zh-cn','en'])for(const [operator_id,reason,parameter,field]of [["get_area", "operator_input_invalid", "input_gdf", ""], ["get_area", "operator_crs_invalid", "", ""], ["get_area", "operator_geometry_field_conflict", "input_gdf", "area"], ["get_length", "operator_input_invalid", "input_gdf", ""], ["get_length", "operator_crs_invalid", "", ""], ["get_length", "operator_geometry_field_conflict", "input_gdf", "length"], ["split_by_area", "operator_input_invalid", "input_gdf", ""], ["split_by_area", "operator_crs_invalid", "", ""], ["split_by_area", "operator_geometry_field_conflict", "input_gdf", "area"], ["split_by_area", "operator_nonnegative_number_required", "threshold", ""]])
 test(`measurement input diagnostics in monitor (${operator_id}, ${reason}, ${field}, ${locale})`,async({page})=>{
    await page.addInitScript(()=>{window.copiedDiagnostic='';Object.defineProperty(navigator,'clipboard',{value:{writeText:async text=>{window.copiedDiagnostic=text}},configurable:true})})
    const details={category:'execution_failed',code:'develop.workflow.invalid_params',message:'PRIVATE_BIZ_FIELD private-password /private/input.gpkg',
      diagnostic:{schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:2,operator_id,...(parameter?{parameter}:{}),...(field?{field}:{})}}}
    await installBackend(page,{locale,empty:true,targetExists:true,errorDetails:details})
    await page.goto('/executions?execution_id=diagnostic-run')
    const panel=page.locator('.execution-detail-content > .execution-failure')
    const names=locale==='zh-cn'?{get_area:'计算面积',get_length:'计算长度',split_by_area:'按面积分割'}:{get_area:'Calculate area',get_length:'Calculate length',split_by_area:'Split by area'}
    await expect(panel).toContainText(names[operator_id])
    await expect(panel).toContainText(locale==='zh-cn'?'定义中的第 2 个节点':'Node 2 in the workflow definition')
    if(parameter)await expect(panel).toContainText(parameter)
    if(reason==='operator_input_invalid'){await expect(panel).toContainText('EMPTY');await expect(panel).toContainText('NULL');await expect(panel).toContainText('Z/M')}
    if(reason==='operator_crs_invalid')await expect(panel).toContainText(locale==='zh-cn'?'原单位平方':'their square')
    if(field){await expect(panel).toContainText(field);await expect(panel).toContainText(locale==='zh-cn'?'手动重命名':'Manually rename')}
    if(parameter==='threshold'){await expect(panel).toContainText(locale==='zh-cn'?'非负':'non-negative');await expect(panel).toContainText('large');await expect(panel).toContainText('small')}
    await expect(panel).not.toContainText('PRIVATE_BIZ_FIELD');await expect(panel).not.toContainText('private-password')
    await panel.getByRole('button',{name:locale==='zh-cn'?'复制诊断信息':'Copy diagnostics',exact:true}).click()
    const copied=await page.evaluate(()=>window.copiedDiagnostic)
    expect(copied).toContain(names[operator_id]);if(parameter)expect(copied).toContain(parameter)
    expect(copied).not.toMatch(/PRIVATE_BIZ_FIELD|private-password|\/private\/input.gpkg/)
    await page.setViewportSize({width:390,height:844})
    expect(await panel.evaluate(el=>el.scrollWidth<=el.clientWidth+1)).toBeTruthy()
 })


for(const locale of ['zh-cn','en'])for(const [reason,parameter,field]of [["operator_input_invalid", "input_gdf", ""], ["operator_reference_invalid", "target_geom", ""], ["operator_geometry_field_conflict", "input_gdf", "distance"], ["operator_projected_crs_required", "", ""], ["operator_crs_mismatch", "", ""]])
 test(`distance admission diagnostics in monitor (${reason}, ${field}, ${locale})`,async({page})=>{
    await page.addInitScript(()=>{window.copiedDiagnostic='';Object.defineProperty(navigator,'clipboard',{value:{writeText:async text=>{window.copiedDiagnostic=text}},configurable:true})})
    const details={category:'execution_failed',code:'develop.workflow.invalid_params',message:'PRIVATE_BIZ_FIELD private-password /private/input.gpkg',
      diagnostic:{schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:2,operator_id:'distance_to',...(parameter?{parameter}:{}),...(field?{field}:{})}}}
    await installBackend(page,{locale,empty:true,targetExists:true,errorDetails:details})
    await page.goto('/executions?execution_id=diagnostic-run')
    const panel=page.locator('.execution-detail-content > .execution-failure')
    const name=locale==='zh-cn'?'距离计算':'Distance calculation'
    await expect(panel).toContainText(name)
    await expect(panel).toContainText(locale==='zh-cn'?'定义中的第 2 个节点':'Node 2 in the workflow definition')
    if(parameter)await expect(panel).toContainText(parameter)
    if(reason==='operator_input_invalid'||reason==='operator_reference_invalid'){
      await expect(panel).toContainText('NULL');await expect(panel).toContainText('EMPTY');await expect(panel).toContainText('Z/M')
    }
    if(reason==='operator_reference_invalid')await expect(panel).toContainText(locale==='zh-cn'?'空源仍验证目标':'even for an empty source')
    if(reason==='operator_projected_crs_required')await expect(panel).toContainText(locale==='zh-cn'?'原水平单位':'original horizontal CRS units')
    if(field){await expect(panel).toContainText(field);await expect(panel).toContainText(locale==='zh-cn'?'手动重命名':'Rename')}
    await expect(panel).not.toContainText('PRIVATE_BIZ_FIELD');await expect(panel).not.toContainText('private-password')
    await panel.getByRole('button',{name:locale==='zh-cn'?'复制诊断信息':'Copy diagnostics',exact:true}).click()
    const copied=await page.evaluate(()=>window.copiedDiagnostic)
    expect(copied).toContain(name);if(parameter)expect(copied).toContain(parameter)
    expect(copied).not.toMatch(/PRIVATE_BIZ_FIELD|private-password|\/private\/input.gpkg/)
    await page.setViewportSize({width:390,height:844})
    expect(await panel.evaluate(el=>el.scrollWidth<=el.clientWidth+1)).toBeTruthy()
 })

for(const locale of ['zh-cn','en'])for(const [reason,parameter,field]of [["operator_input_invalid", "input_gdf", ""], ["operator_crs_invalid", "", ""], ["operator_geometry_field_conflict", "input_gdf", "minx"], ["operator_geometry_field_conflict", "input_gdf", "miny"], ["operator_geometry_field_conflict", "input_gdf", "maxx"], ["operator_geometry_field_conflict", "input_gdf", "maxy"]])
 test(`bounds admission diagnostics in monitor (${reason}, ${field}, ${locale})`,async({page})=>{
    await page.addInitScript(()=>{window.copiedDiagnostic='';Object.defineProperty(navigator,'clipboard',{value:{writeText:async text=>{window.copiedDiagnostic=text}},configurable:true})})
    const details={category:'execution_failed',code:'develop.workflow.invalid_params',message:'PRIVATE_BIZ_FIELD private-password /private/input.gpkg',
      diagnostic:{schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:2,operator_id:'get_bounds',...(parameter?{parameter}:{}),...(field?{field}:{})}}}
    await installBackend(page,{locale,empty:true,targetExists:true,errorDetails:details})
    await page.goto('/executions?execution_id=diagnostic-run')
    const panel=page.locator('.execution-detail-content > .execution-failure')
    const name=locale==='zh-cn'?'获取边界框':'Get bounds'
    await expect(panel).toContainText(name)
    await expect(panel).toContainText(locale==='zh-cn'?'定义中的第 2 个节点':'Node 2 in the workflow definition')
    if(parameter)await expect(panel).toContainText(parameter)
    if(reason==='operator_input_invalid'){await expect(panel).toContainText('NULL');await expect(panel).toContainText('EMPTY');await expect(panel).toContainText('Z/M')}
    if(reason==='operator_crs_invalid')await expect(panel).toContainText(locale==='zh-cn'?'地理或投影CRS':'geographic or projected CRS')
    if(field){await expect(panel).toContainText(field);await expect(panel).toContainText(locale==='zh-cn'?'手动重命名':'Rename')}
    await expect(panel).not.toContainText('PRIVATE_BIZ_FIELD');await expect(panel).not.toContainText('private-password')
    await panel.getByRole('button',{name:locale==='zh-cn'?'复制诊断信息':'Copy diagnostics',exact:true}).click()
    const copied=await page.evaluate(()=>window.copiedDiagnostic)
    expect(copied).toContain(name);if(parameter)expect(copied).toContain(parameter)
    expect(copied).not.toMatch(/PRIVATE_BIZ_FIELD|private-password|\/private\/input.gpkg/)
    await page.setViewportSize({width:390,height:844})
    expect(await panel.evaluate(el=>el.scrollWidth<=el.clientWidth+1)).toBeTruthy()
 })


for(const locale of ['zh-cn','en'])for(const [reason,parameter,field]of [["operator_input_invalid", "input_gdf", ""], ["operator_crs_invalid", "", ""], ["operator_field_conflict", "input_gdf", "source_id"], ["operator_field_conflict", "input_gdf", "part_no"]])
 test(`multipart split diagnostics in monitor (${reason}, ${field}, ${locale})`,async({page})=>{
    await page.addInitScript(()=>{window.copiedDiagnostic='';Object.defineProperty(navigator,'clipboard',{value:{writeText:async text=>{window.copiedDiagnostic=text}},configurable:true})})
    const details={category:'execution_failed',code:'develop.workflow.invalid_params',message:'PRIVATE_BIZ_FIELD private-password /private/input.gpkg',
      diagnostic:{schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:2,operator_id:'split_multipart',...(parameter?{parameter}:{}),...(field?{field}:{})}}}
    await installBackend(page,{locale,empty:true,targetExists:true,errorDetails:details})
    await page.goto('/executions?execution_id=diagnostic-run')
    const panel=page.locator('.execution-detail-content > .execution-failure')
    const name=locale==='zh-cn'?'多部拆分':'Multipart split'
    await expect(panel).toContainText(name)
    await expect(panel).toContainText(locale==='zh-cn'?'定义中的第 2 个节点':'Node 2 in the workflow definition')
    if(parameter)await expect(panel).toContainText(parameter)
    if(reason==='operator_input_invalid'){for(const token of ['NULL','EMPTY','Z/M','GeometryCollection','id_field'])await expect(panel).toContainText(token)}
    if(reason==='operator_crs_invalid')await expect(panel).toContainText(locale==='zh-cn'?'地理或投影CRS':'geographic or projected CRS')
    if(field){await expect(panel).toContainText(field);await expect(panel).toContainText(locale==='zh-cn'?'手动重命名':'Rename')}
    await expect(panel).not.toContainText('PRIVATE_BIZ_FIELD');await expect(panel).not.toContainText('private-password')
    await panel.getByRole('button',{name:locale==='zh-cn'?'复制诊断信息':'Copy diagnostics',exact:true}).click()
    const copied=await page.evaluate(()=>window.copiedDiagnostic)
    expect(copied).toContain(name);if(parameter)expect(copied).toContain(parameter)
    expect(copied).not.toMatch(/PRIVATE_BIZ_FIELD|private-password|\/private\/input.gpkg/)
    await page.setViewportSize({width:390,height:844})
    expect(await panel.evaluate(el=>el.scrollWidth<=el.clientWidth+1)).toBeTruthy()
 })


for(const locale of ['zh-cn','en'])for(const [reason,parameter,field]of [["operator_input_invalid", "input_gdf", ""], ["operator_crs_invalid", "", ""], ["operator_field_conflict", "input_gdf", "source_id"], ["operator_field_conflict", "input_gdf", "part_no"], ["operator_field_conflict", "input_gdf", "ring_no"], ["operator_field_conflict", "input_gdf", "vertex_no"], ["operator_field_conflict", "input_gdf", "vertex_id"]])
 test(`vertices extraction diagnostics in monitor (${reason}, ${field}, ${locale})`,async({page})=>{
    await page.addInitScript(()=>{window.copiedDiagnostic='';Object.defineProperty(navigator,'clipboard',{value:{writeText:async text=>{window.copiedDiagnostic=text}},configurable:true})})
    const details={category:'execution_failed',code:'develop.workflow.invalid_params',message:'PRIVATE_BIZ_FIELD private-password /private/input.gpkg',
      diagnostic:{schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:2,operator_id:'extract_vertices',...(parameter?{parameter}:{}),...(field?{field}:{})}}}
    await installBackend(page,{locale,empty:true,targetExists:true,errorDetails:details})
    await page.goto('/executions?execution_id=diagnostic-run')
    const panel=page.locator('.execution-detail-content > .execution-failure')
    const name=locale==='zh-cn'?'顶点提取':'Extract vertices'
    await expect(panel).toContainText(name)
    await expect(panel).toContainText(locale==='zh-cn'?'定义中的第 2 个节点':'Node 2 in the workflow definition')
    if(parameter)await expect(panel).toContainText(parameter)
    if(reason==='operator_input_invalid'){for(const token of ['NULL','EMPTY','Z/M','GeometryCollection','id_field'])await expect(panel).toContainText(token)}
    if(reason==='operator_crs_invalid')await expect(panel).toContainText(locale==='zh-cn'?'地理或投影CRS':'geographic or projected CRS')
    if(field){await expect(panel).toContainText(field);await expect(panel).toContainText(locale==='zh-cn'?'手动重命名':'Rename')}
    await expect(panel).not.toContainText('PRIVATE_BIZ_FIELD');await expect(panel).not.toContainText('private-password')
    await panel.getByRole('button',{name:locale==='zh-cn'?'复制诊断信息':'Copy diagnostics',exact:true}).click()
    const copied=await page.evaluate(()=>window.copiedDiagnostic)
    expect(copied).toContain(name);if(parameter)expect(copied).toContain(parameter)
    expect(copied).not.toMatch(/PRIVATE_BIZ_FIELD|private-password|\/private\/input.gpkg/)
    await page.setViewportSize({width:390,height:844})
    expect(await panel.evaluate(el=>el.scrollWidth<=el.clientWidth+1)).toBeTruthy()
 })


for(const locale of ['zh-cn','en'])for(const [operator_id,reason,parameter,field]of [["check_geometry", "operator_input_invalid", "input_gdf", ""], ["check_geometry", "operator_output_fields_invalid", "", ""], ["repair_geometry", "operator_input_invalid", "input_gdf", ""]])
 test(`geometry validity diagnostics in monitor (${operator_id}, ${reason}, ${locale})`,async({page})=>{
    await page.addInitScript(()=>{window.copiedDiagnostic='';Object.defineProperty(navigator,'clipboard',{value:{writeText:async text=>{window.copiedDiagnostic=text}},configurable:true})})
    const details={category:'execution_failed',code:'develop.workflow.invalid_params',message:'PRIVATE_BIZ_FIELD private-password /private/input.gpkg',
      diagnostic:{schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:2,operator_id,...(parameter?{parameter}:{}),...(field?{field}:{})}}}
    await installBackend(page,{locale,empty:true,targetExists:true,errorDetails:details})
    await page.goto('/executions?execution_id=diagnostic-run')
    const panel=page.locator('.execution-detail-content > .execution-failure')
    const name=operator_id==='check_geometry'?(locale==='zh-cn'?'几何检查':'Check geometry'):(locale==='zh-cn'?'几何修复':'Repair geometry')
    await expect(panel).toContainText(name)
    await expect(panel).toContainText(locale==='zh-cn'?'定义中的第 2 个节点':'Node 2 in the workflow definition')
    if(parameter)await expect(panel).toContainText(parameter)
    if(reason==='operator_input_invalid'){for(const token of ['NULL/EMPTY','XY','Z/M'])await expect(panel).toContainText(token)}
    if(reason==='operator_output_fields_invalid'){await expect(panel).toContainText('valid_field');await expect(panel).toContainText('reason_field')}
    if(field){await expect(panel).toContainText(field);await expect(panel).toContainText(locale==='zh-cn'?'手动重命名':'Rename')}
    await expect(panel).not.toContainText('PRIVATE_BIZ_FIELD');await expect(panel).not.toContainText('private-password')
    await panel.getByRole('button',{name:locale==='zh-cn'?'复制诊断信息':'Copy diagnostics',exact:true}).click()
    const copied=await page.evaluate(()=>window.copiedDiagnostic)
    expect(copied).toContain(name);if(parameter)expect(copied).toContain(parameter)
    expect(copied).not.toMatch(/PRIVATE_BIZ_FIELD|private-password|\/private\/input.gpkg/)
    await page.setViewportSize({width:390,height:844})
    expect(await panel.evaluate(el=>el.scrollWidth<=el.clientWidth+1)).toBeTruthy()
 })


for(const locale of ['zh-cn','en'])for(const [operator_id,reason,parameter,field]of [["polygonize", "operator_input_invalid", "input_gdf", ""], ["snap_geometry", "operator_input_invalid", "input_gdf", ""], ["snap_geometry", "operator_reference_invalid", "reference_gdf", ""], ["snap_geometry", "operator_crs_invalid", "", ""], ["snap_geometry", "operator_nonnegative_number_required", "tolerance", ""]])
 test(`${operator_id === 'polygonize' ? 'polygonize' : 'reference snap'} diagnostics in monitor (${operator_id}, ${reason}, ${locale})`,async({page})=>{
    await page.addInitScript(()=>{window.copiedDiagnostic='';Object.defineProperty(navigator,'clipboard',{value:{writeText:async text=>{window.copiedDiagnostic=text}},configurable:true})})
    const details={category:'execution_failed',code:'develop.workflow.invalid_params',message:'PRIVATE_BIZ_FIELD private-password /private/input.gpkg',
      diagnostic:{schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:2,operator_id,...(parameter?{parameter}:{}),...(field?{field}:{})}}}
    await installBackend(page,{locale,empty:true,targetExists:true,errorDetails:details})
    await page.goto('/executions?execution_id=diagnostic-run')
    const panel=page.locator('.execution-detail-content > .execution-failure')
    const name = operator_id === 'polygonize'
      ? (locale === 'zh-cn' ? '拓扑构面' : 'Polygonize linework')
      : (locale === 'zh-cn' ? '参考吸附' : 'Reference snap')
    await expect(panel).toContainText(name)
    await expect(panel).toContainText(locale==='zh-cn'?'定义中的第 2 个节点':'Node 2 in the workflow definition')
    if(parameter)await expect(panel).toContainText(parameter)
    if (operator_id === 'polygonize') {
      for (const token of ['LineString', 'MultiLineString', 'NULL', 'Z/M']) await expect(panel).toContainText(token)
    } else if(['operator_input_invalid','operator_reference_invalid'].includes(reason)){for(const token of ['NULL/EMPTY','XY','Z/M'])await expect(panel).toContainText(token)}
    if(reason==='operator_nonnegative_number_required')await expect(panel).toContainText(locale==='zh-cn'?'零容差':'Zero')
    if(reason==='operator_crs_invalid')await expect(panel).toContainText('CRS')
    if(field){await expect(panel).toContainText(field);await expect(panel).toContainText(locale==='zh-cn'?'手动重命名':'Rename')}
    await expect(panel).not.toContainText('PRIVATE_BIZ_FIELD');await expect(panel).not.toContainText('private-password')
    await panel.getByRole('button',{name:locale==='zh-cn'?'复制诊断信息':'Copy diagnostics',exact:true}).click()
    const copied=await page.evaluate(()=>window.copiedDiagnostic)
    expect(copied).toContain(name);if(parameter)expect(copied).toContain(parameter)
    expect(copied).not.toMatch(/PRIVATE_BIZ_FIELD|private-password|\/private\/input.gpkg/)
    await page.setViewportSize({width:390,height:844})
    expect(await panel.evaluate(el=>el.scrollWidth<=el.clientWidth+1)).toBeTruthy()
 })
