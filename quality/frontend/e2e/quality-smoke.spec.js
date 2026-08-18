import { expect, test } from '@playwright/test'

const executionID = 'bb1a324d-53a3-4f02-b666-d51385d258c8'
const ruleKey = 'bfed8f3b-e3b4-8a8e-8140-860d0b0585ea'

const engines = [
  { id: 2, name: '业务 PostgreSQL', engine_type: 'postgresql', lifecycle_state: 'active' }
]

const qualityResult = {
  schema_version: 'addp.quality.execution-result/v1',
  quality_score: 86.67,
  total_rules: 3,
  passed_rules: 2,
  failed_rules: 1,
  field_scores: [{ column: 'mobile_phone', score: 86.67, rule_count: 3 }],
  rule_details: [{
    rule_application_id: 9,
    rule_key: ruleKey,
    type: 'format',
    severity: 'error',
    message: '手机号格式不正确',
    column: 'mobile_phone',
    table: 'customers',
    schema: 'public',
    pass_rate: 86.67,
    failed_count: 4,
    total_count: 30,
    passed: false
  }]
}

const executions = [{
  execution_id: executionID,
  source_task_name: '客户表质量检查',
  status: 'success',
  execution_time_ms: 142,
  created_at: '2026-08-14T08:00:00Z',
  metadata: qualityResult
}]

const issues = [{
  id: 17,
  type: 'format',
  rule_key: ruleKey,
  table_name: 'customers',
  column_name: 'mobile_phone',
  pass_rate: 86.67,
  failed_count: 4,
  execution_id: executionID,
  last_execution_id: executionID,
  engine_id: 2,
  status: 'ignored',
  created_at: '2026-08-14T08:00:00Z'
}]

const ruleApplications = [{
  id: 9,
  element: { id: 3, name: '手机号', code: 'mobile_phone' },
  engine_id: 2,
  schema_name: 'public',
  table_name: 'customers',
  column_name: 'mobile_phone',
  enabled: true
}]

const checkTasks = [{
  id: 12,
  name: '客户表质量检查',
  description: '客户核心字段质量检查',
  engine_id: 2,
  schema_name: 'public',
  table_name: 'customers',
  last_execution_id: executionID,
  last_execution_status: 'success',
  last_run_at: '2026-08-14T08:00:00Z'
}]

test('loads Quality management pages with stable business fields', async ({ page }) => {
  await installMockBackend(page)

  await page.goto('/executions?status=success&page_size=50')
  await expect(page.getByRole('heading', { name: '执行记录' })).toBeVisible()
  await expect(page.getByText(executionID, { exact: true }).first()).toBeVisible()
  await expect(page.getByText('86.7%', { exact: true }).first()).toBeVisible()

  await page.goto('/issues?status=ignored&engine_id=2&page_size=50')
  await expect(page.getByRole('heading', { name: '问题工单' })).toBeVisible()
  await expect(page.getByText('mobile_phone', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('已忽略', { exact: true }).first()).toBeVisible()

  await page.goto('/rule-applications?engine_id=2&schema_name=public&table_name=customers&page_size=50')
  await expect(page.getByRole('heading', { name: '规则应用配置' })).toBeVisible()
  await expect(page.getByText('手机号（mobile_phone）', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('customers', { exact: true }).first()).toBeVisible()

  await page.goto('/check-tasks?page_size=50')
  await expect(page.getByRole('heading', { name: '检查任务' })).toBeVisible()
  await expect(page.getByText('客户表质量检查', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('查看详情', { exact: true }).first()).toBeVisible()
})

test('preserves execution list filters through detail and shows rule identity', async ({ page }) => {
  await installMockBackend(page)
  await page.goto('/executions?status=success&page_size=50')

  await page.getByRole('button', { name: '详情' }).click()
  await expect(page).toHaveURL(new RegExp(`/executions/${executionID}\\?status=success&page_size=50$`))
  await expect(page.getByText('执行详情', { exact: false }).first()).toBeVisible()
  await expect(page.getByText(ruleKey, { exact: true }).first()).toBeVisible()
  await expect(page.getByText('86.7%', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('format', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('4', { exact: true }).first()).toBeVisible()

  await page.locator('.el-page-header__back').click()
  await expect(page).toHaveURL(/\/executions\?status=success&page_size=50$/)
  await expect(page.getByText(executionID, { exact: true }).first()).toBeVisible()
})

test('renders the filtered-empty state when no execution matches', async ({ page }) => {
  await installMockBackend(page)
  await page.goto('/executions?status=failed&page_size=50')
  await expect(page.getByText('没有符合筛选条件的执行记录', { exact: true })).toBeVisible()
})

test('persists rule application enable changes', async ({ page }) => {
  const backend = await installMockBackend(page, { ruleApplicationEnabled: false })
  await page.goto('/rule-applications')

  const enabledSwitch = page.getByRole('switch', { name: '启用' }).first()
  await expect(enabledSwitch).toHaveAttribute('aria-checked', 'false')
  await page.locator('.el-switch').first().click()

  await expect.poll(() => backend.enabledRequests.length).toBe(1)
  expect(backend.enabledRequests[0]).toEqual({ id: 9, enabled: true })
  await expect(enabledSwitch).toHaveAttribute('aria-checked', 'true')

  await page.locator('.el-switch').first().click()
  await expect.poll(() => backend.enabledRequests.length).toBe(2)
  expect(backend.enabledRequests[1]).toEqual({ id: 9, enabled: false })
  await expect(enabledSwitch).toHaveAttribute('aria-checked', 'false')
})

test('creates and starts a check task through the catalog-backed form', async ({ page }) => {
  const backend = await installMockBackend(page)
  await page.goto('/check-tasks')
  await page.getByRole('button', { name: '新建任务' }).click()

  const dialog = page.getByRole('dialog', { name: '新建检查任务' })
  await expect(page).toHaveURL(/\/check-tasks\?create=1$/)
  await fillCheckTaskForm(page, dialog, '新增客户质量检查')
  await dialog.getByRole('button', { name: '确定' }).click()

  await expect.poll(() => backend.taskCreateRequests.length).toBe(1)
  expect(backend.taskCreateRequests[0]).toMatchObject({
    name: '新增客户质量检查',
    engine_id: 2,
    schema_name: 'public',
    table_name: 'customers'
  })
  const createdRow = page.getByRole('row', { name: /新增客户质量检查/ })
  await expect(createdRow).toBeVisible()

  await createdRow.getByRole('button', { name: '执行' }).click()
  await expect.poll(() => backend.taskRunRequests.length).toBe(1)
  expect(backend.taskRunRequests[0]).toBe(13)
  await expect(createdRow.getByText('待执行', { exact: true })).toBeVisible()
})

test('requires and persists a note when resolving an issue', async ({ page }) => {
  const backend = await installMockBackend(page, { issueStatus: 'open' })
  await page.goto('/issues')

  await page.getByRole('button', { name: '标记解决' }).click()
  const prompt = page.getByRole('dialog', { name: '填写处理说明' })
  await prompt.getByRole('textbox').fill('已修复手机号格式校验')
  await prompt.getByRole('button', { name: '确定' }).click()

  await expect.poll(() => backend.issueStatusRequests.length).toBe(1)
  expect(backend.issueStatusRequests[0]).toEqual({
    id: 17,
    status: 'resolved',
    note: '已修复手机号格式校验'
  })
  await expect(page.getByText('已解决', { exact: true }).first()).toBeVisible()
  await expect(page.getByRole('button', { name: '标记解决' })).toHaveCount(0)
})

test('requires and persists a note when ignoring an issue', async ({ page }) => {
  const backend = await installMockBackend(page, { issueStatus: 'open' })
  await page.goto('/issues')

  await page.getByRole('button', { name: '忽略' }).click()
  const prompt = page.getByRole('dialog', { name: '填写处理说明' })
  await prompt.getByRole('textbox').fill('业务确认该异常可忽略')
  await prompt.getByRole('button', { name: '确定' }).click()

  await expect.poll(() => backend.issueStatusRequests.length).toBe(1)
  expect(backend.issueStatusRequests[0]).toEqual({
    id: 17,
    status: 'ignored',
    note: '业务确认该异常可忽略'
  })
  await expect(page.getByText('已忽略', { exact: true }).first()).toBeVisible()
  await expect(page.getByRole('button', { name: '忽略' })).toHaveCount(0)
})

test('rolls rule application state back when the update is rejected', async ({ page }) => {
  const backend = await installMockBackend(page, {
    ruleApplicationEnabled: false,
    ruleApplicationUpdateError: '规则应用已被其他用户修改'
  })
  await page.goto('/rule-applications')

  const enabledSwitch = page.getByRole('switch', { name: '启用' }).first()
  await page.locator('.el-switch').first().click()
  await expect.poll(() => backend.enabledRequests.length).toBe(1)
  await expect(enabledSwitch).toHaveAttribute('aria-checked', 'false')
  await expect(page.getByText('规则应用已被其他用户修改', { exact: true })).toBeVisible()
})

test('keeps a valid check task form open when creation is rejected', async ({ page }) => {
  const backend = await installMockBackend(page, { taskCreateError: '同名检查任务已存在' })
  await page.goto('/check-tasks')
  await page.getByRole('button', { name: '新建任务' }).click()

  const dialog = page.getByRole('dialog', { name: '新建检查任务' })
  await fillCheckTaskForm(page, dialog, '重复名称质量检查')
  await dialog.getByRole('button', { name: '确定' }).click()

  await expect.poll(() => backend.taskCreateRequests.length).toBe(1)
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('textbox').first()).toHaveValue('重复名称质量检查')
  await expect(page.getByText('同名检查任务已存在', { exact: true })).toBeVisible()
  await expect(page.getByRole('row', { name: /重复名称质量检查/ })).toHaveCount(0)
})

test('keeps an issue open when its status update is rejected', async ({ page }) => {
  const backend = await installMockBackend(page, {
    issueStatus: 'open',
    issueStatusError: '问题工单已被处理'
  })
  await page.goto('/issues')

  await page.getByRole('button', { name: '标记解决' }).click()
  const prompt = page.getByRole('dialog', { name: '填写处理说明' })
  await prompt.getByRole('textbox').fill('尝试解决')
  await prompt.getByRole('button', { name: '确定' }).click()

  await expect.poll(() => backend.issueStatusRequests.length).toBe(1)
  await expect(page.getByText('问题工单已被处理', { exact: true })).toBeVisible()
  await expect(page.getByText('待处理', { exact: true }).first()).toBeVisible()
  await expect(page.getByRole('button', { name: '标记解决' })).toBeVisible()
})

test('submits a check task create request only once while it is pending', async ({ page }) => {
  const backend = await installMockBackend(page, { taskCreateDelay: 150 })
  await page.goto('/check-tasks')
  await page.getByRole('button', { name: '新建任务' }).click()

  const dialog = page.getByRole('dialog', { name: '新建检查任务' })
  await fillCheckTaskForm(page, dialog, '防重提交质量检查')
  await dialog.getByRole('button', { name: '确定' }).evaluate(button => {
    button.click()
    button.click()
  })

  await expect.poll(() => backend.taskCreateRequests.length).toBe(1)
  await expect(page.getByRole('row', { name: /防重提交质量检查/ })).toBeVisible()
})

async function fillCheckTaskForm(page, dialog, name) {
  await dialog.getByRole('textbox').first().fill(name)
  const selectByLabel = (label) => dialog.locator('.el-form-item').filter({ hasText: label }).locator('.el-select')
  await selectByLabel('PostgreSQL 引擎').click()
  await page.getByRole('option', { name: '业务 PostgreSQL' }).click()
  await selectByLabel('Schema').click()
  await page.getByRole('option', { name: 'public' }).click()
  await selectByLabel('数据表').click()
  await page.getByRole('option', { name: 'customers' }).click()
}

async function installMockBackend(page, options = {}) {
  const state = {
    ruleApplications: ruleApplications.map(item => ({
      ...item,
      element: { ...item.element },
      enabled: options.ruleApplicationEnabled ?? item.enabled
    })),
    issues: issues.map(item => ({ ...item, status: options.issueStatus ?? item.status })),
    checkTasks: checkTasks.map(item => ({ ...item })),
    enabledRequests: [],
    issueStatusRequests: [],
    taskCreateRequests: [],
    taskRunRequests: []
  }

  await page.addInitScript(() => {
    localStorage.setItem('addp-lang', 'zh-cn')
    localStorage.setItem('theme-mode', 'light')
  })

  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname

    if (path === '/api/v1/system/refresh') {
      return fulfillJSON(route, { access_token: 'quality-e2e-token', expires_in: 3600 })
    }
    if (path === '/api/v1/system/users/me') {
      return fulfillJSON(route, { id: 1, username: 'quality-e2e' })
    }
    if (path === '/api/v1/system/auth/context') {
      return fulfillJSON(route, {
        context: { type: 'tenant' },
        authorization: { role_assignments: [{ permissions: [] }] }
      })
    }
    if (path === '/api/v1/system/engines') return fulfillJSON(route, engines)

    if (request.method() === 'POST' && path === '/api/v1/system/engines/2/catalog/children') {
      const segments = request.postDataJSON()?.path?.segments || []
      if (segments.length === 0) {
        return fulfillJSON(route, { nodes: [{ name: '数据库', role: 'branch', path: { segments: ['database'] } }] })
      }
      if (segments.length === 1) {
        return fulfillJSON(route, { nodes: [{ name: 'public', role: 'branch', path: { segments: ['database', 'public'] } }] })
      }
      return fulfillJSON(route, { nodes: [{ name: 'customers', role: 'leaf', path: { segments: ['database', 'public', 'customers'] } }] })
    }

    if (request.method() === 'PUT' && path === '/api/v1/quality/rule-applications/9') {
      const body = request.postDataJSON()
      const row = state.ruleApplications.find(item => item.id === 9)
      state.enabledRequests.push({ id: 9, enabled: Boolean(body.enabled) })
      if (options.ruleApplicationUpdateError) {
        return fulfillJSON(route, { error: options.ruleApplicationUpdateError }, 409)
      }
      row.enabled = Boolean(body.enabled)
      return fulfillJSON(route, row)
    }
    if (request.method() === 'POST' && path === '/api/v1/quality/check-tasks') {
      const body = request.postDataJSON()
      state.taskCreateRequests.push(body)
      if (options.taskCreateDelay) {
        await new Promise(resolve => setTimeout(resolve, options.taskCreateDelay))
      }
      if (options.taskCreateError) {
        return fulfillJSON(route, { error: options.taskCreateError }, 409)
      }
      const task = { id: 13, ...body, last_execution_id: null, last_execution_status: null, last_run_at: null }
      state.checkTasks.push(task)
      return fulfillJSON(route, task, 201)
    }
    if (request.method() === 'POST' && path === '/api/v1/quality/check-tasks/13/run') {
      state.taskRunRequests.push(13)
      const task = state.checkTasks.find(item => item.id === 13)
      task.last_execution_id = 'execution-created-13'
      task.last_execution_status = 'pending'
      return fulfillJSON(route, { execution_id: 'execution-created-13' })
    }
    if (request.method() === 'PUT' && path === '/api/v1/quality/issues/17/status') {
      const body = request.postDataJSON()
      const issue = state.issues.find(item => item.id === 17)
      state.issueStatusRequests.push({ id: 17, status: body.status, note: body.note })
      if (options.issueStatusError) {
        return fulfillJSON(route, { error: options.issueStatusError }, 409)
      }
      issue.status = body.status
      return fulfillJSON(route, issue)
    }

    if (path === '/api/v1/quality/executions') {
      const data = url.searchParams.get('status') === 'failed' ? [] : executions
      return fulfillJSON(route, { data, total: data.length })
    }
    if (path === `/api/v1/quality/executions/${executionID}`) return fulfillJSON(route, executions[0])
    if (path === '/api/v1/quality/issues') {
      const requestedStatus = url.searchParams.get('status')
      const data = state.issues.filter(issue => !requestedStatus || issue.status === requestedStatus)
      return fulfillJSON(route, { data, total: data.length })
    }
    if (path === '/api/v1/quality/rule-applications') {
      return fulfillJSON(route, { data: state.ruleApplications, total: state.ruleApplications.length })
    }
    if (path === '/api/v1/quality/check-tasks') {
      return fulfillJSON(route, { data: state.checkTasks, total: state.checkTasks.length })
    }
    return fulfillJSON(route, {})
  })

  return state
}

async function fulfillJSON(route, body, status = 200) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body)
  })
}
