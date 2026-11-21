import { createHash } from 'node:crypto'
import { verifyDifferenceLogPages } from './online/difference-execution-logs.mjs'
import { expect, test } from '@playwright/test'
import { mockGraph } from './graph.fixture'

const id = '550e8400-e29b-41d4-a716-446655440001'

const owners = [
  { module: 'transfer', type: 'sync', permission: 'transfer.task.read', result: { task_id: 63, records_read: 20, records_written: 20 }, expected: '执行详情 #' + id },
  { module: 'quality', type: 'quality_plan', permission: 'quality.plan.read', result: { execution_config: { target_key: 'quality-sample' } }, expected: 'quality-sample' },
  { module: 'meta', type: 'scan', permission: 'meta.scan_task.read', result: { scan_context: { engine_id: 177, scan_depth: 'deep' } }, expected: '177' },
  { module: 'manager', type: 'embedding', permission: 'manager.derived_artifact.read', result: { metadata: { generated: 11 } }, expected: '11' },
  { module: 'orchestrator', type: 'orchestration', permission: 'orchestrator.workflow.read', result: { metadata: { step_results: { 'scan-orders': { status: 'success' } } } }, expected: 'scan-orders - success' }
]

for (const owner of owners) for (const canRead of [true, false]) test(`${owner.module} result navigation uses the existing read permission (${canRead})`, async ({ page }) => {
  const execution = {
    id: 1256, execution_id: id, module: owner.module, task_type: owner.type, source: 'orchestrator',
    source_task_id: '63', source_task_name: '同步任务', status: 'success', progress: 100,
    attempt: 1, created_at: '2026-11-18T02:32:48Z', metadata: {}, steps: []
  }
  const unexpected = await mockGraph(page, { permissions: ['monitor.execution.read', ...(canRead ? [owner.permission] : [])], responses: {
    '/api/v1/monitor/task-providers': [],
    '/api/v1/monitor/executions': { executions: [execution], total: 150, page: 3, page_size: 50 },
    [`/api/v1/monitor/executions/by-execution-id/${id}/tree`]: { execution, children: [], truncated: false },
    [`/api/v1/monitor/executions/by-execution-id/${id}/events`]: { items: [], has_more: false, next_cursor: 0 },
    [`/api/v1/${owner.module}/executions/${id}`]: { ...execution, ...owner.result },
    [`/api/v1/transfer/executions/${id}/events`]: { items: [], has_more: false, next_cursor: 0 }
  } })
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  const monitorURL = `/monitor/executions?module=${owner.module}&task_type=${owner.type}&source_task_id=63&page=3&page_size=50&execution_id=${id}`
  await page.goto(monitorURL)
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  const dialog = frame.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: '同步任务', exact: true })).toBeVisible()
  if (owner.module === 'transfer') await expect(dialog.getByRole('button', { name: '打开任务定义', exact: true })).toBeVisible()
  const result = dialog.getByRole('button', { name: '查看执行结果', exact: true })
  if (canRead) {
    await result.click({ timeout: 5000 })
    await expect(page).toHaveURL(url => url.pathname === `/${owner.module}/executions/${id}` && url.searchParams.get('return_to') === monitorURL)
    await expect(frame.getByText(owner.expected, { exact: true })).toBeVisible()
    await page.reload()
    await frame.getByRole('button', { name: '返回本次执行', exact: true }).click()
    await expect(page).toHaveURL(monitorURL)
    await expect(dialog.getByRole('heading', { name: '同步任务', exact: true })).toBeVisible()
  } else {
    await expect(result).toHaveCount(0)
  }
  expect(unexpected).toEqual([])
})

test('an empty Owner result and a failed refresh keep distinct states and the same return target', async ({ page }) => {
  const path = '/api/v1/manager/executions/' + id
  const unexpected = await mockGraph(page, {
    permissions: ['monitor.execution.read', 'manager.derived_artifact.read'],
    responses: { [path]: { execution_id: id, status: 'success', metadata: {} } }
  })
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  const monitorURL = '/monitor/executions?module=manager&page=3&execution_id=' + id
  await page.goto('/manager/executions/' + id + '?' + new URLSearchParams({ return_to: monitorURL }))
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(frame.getByText('本次执行暂无已记录的结果明细')).toBeVisible()
  await page.route('**' + path, route => route.fulfill({ status: 503, json: { error: 'unavailable' } }))
  await frame.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(frame.getByText('无法读取本次执行结果')).toBeVisible()
  await expect(frame.getByText('本次执行暂无已记录的结果明细')).toHaveCount(0)
  await expect(frame.getByRole('button', { name: '返回本次执行' })).toBeEnabled()
  expect(unexpected).toEqual([])
})

for (const status of ['pending', 'running', 'success']) test(`same ${status} execution survives task, Monitor, result, reload and return`, async ({ page }) => {
  const execution = { id: 1256, execution_id: id, module: 'develop', task_type: 'script', source: 'orchestrator', source_task_id: '37', source_task_name: '导航任务', status, progress: status === 'success' ? 100 : 0, attempt: 1, created_at: '2026-11-14T01:00:00Z', execution_config: { inputs: {} }, metadata: { result: { fixture_value: 42 } }, dev_task: { name: '导航任务' }, steps: [] }
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  const unexpected = await mockGraph(page, { permissions: ['develop.task.read', 'develop.notebook.read', 'monitor.execution.read'], responses: {
    '/api/v1/develop/task-definitions': { items: [{ id: 37, name: '导航任务', dev_type: 'script', status: 'active', last_execution_id: id, last_execution_status: status, last_run_at: execution.created_at }], total: 1 },
    '/api/v1/develop/engines': { data: [] },
    '/api/v1/monitor/task-providers': [],
    '/api/v1/monitor/executions': { executions: [execution], total: 150, page: 3, page_size: 50 },
    [`/api/v1/monitor/executions/by-execution-id/${id}/tree`]: { execution, children: [], truncated: false },
    [`/api/v1/monitor/executions/by-execution-id/${id}/events`]: { items: [], has_more: false, next_cursor: 0 },
    [`/api/v1/develop/executions/${id}`]: execution,
    [`/api/v1/develop/executions/${id}/logs`]: []
  } })
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  await page.goto('/develop/tasks')
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(frame.getByRole('heading', { name: '任务管理', exact: true })).toBeVisible()
  await frame.getByRole('button', { name: '最近执行', exact: true }).click()
  await expect(page).toHaveURL('/monitor/executions?execution_id=' + id)
  await expect(frame.getByRole('dialog')).toBeVisible()
  await expect(frame.locator('.execution-technical')).not.toHaveAttribute('open', '')
  const monitorURL = '/monitor/executions?module=develop&task_type=script&source_task_id=37&page=3&page_size=50&execution_id=' + id
  await page.goto(monitorURL)
  await frame.getByRole('button', { name: '查看执行结果', exact: true }).click()
  await expect(page).toHaveURL(url => url.pathname === '/develop/executions/' + id && url.searchParams.get('return_to') === monitorURL)
  await expect(frame.getByRole('heading', { name: '执行结果', exact: true })).toBeVisible()
  await expect(frame.locator('.el-descriptions')).toHaveCount(0)
  if (status === 'success') await expect(frame.locator('#pane-result .json-result')).toContainText('42')
  await frame.getByRole('tab', { name: '执行日志', exact: true }).click()
  await expect(page).toHaveURL(url => url.searchParams.get('tab') === 'logs' && url.searchParams.get('return_to') === monitorURL)
  const resultURL = page.url()
  await page.reload()
  await expect(frame.getByRole('heading', { name: '执行结果', exact: true })).toBeVisible()
  await frame.getByRole('button', { name: '返回本次执行', exact: true }).click()
  await expect(page).toHaveURL(monitorURL)
  await expect(frame.getByRole('dialog')).toBeVisible()
  await page.goBack()
  await expect(page).toHaveURL(resultURL)
  await expect(frame.getByRole('heading', { name: '执行结果', exact: true })).toBeVisible()
  await page.goForward()
  await expect(page).toHaveURL(monitorURL)
  await expect(frame.getByRole('dialog')).toBeVisible()
  expect(unexpected).toEqual([])
  expect(errors).toEqual([])
})

// The real runner calls this same DOM oracle; only these T3 HTTP responses are controlled.
for (const state of ['pending', 'running', 'success']) test(`real navigation oracle keeps the same ${state} query and cleanup-free page commands`, async ({ page }) => {
  const { assertExecutionNavigationPage } = await import('./online/execution-navigation-local-support.mjs')
  const old = '550e8400-e29b-41d4-a716-446655440002'
  const execution = { id: 1256, execution_id: id, module: 'develop', task_type: 'query', source: 'develop', source_task_id: '37', source_task_name: '导航查询', triggered_by: 9,
    status: state, progress: state === 'success' ? 100 : 0, attempt: state === 'pending' ? 0 : 1,
    created_at: '2026-11-14T01:00:00Z', started_at: state === 'pending' ? null : '2026-11-14T01:00:01Z', lease_state: state === 'running' ? 'active' : 'none',
    execution_config: { engine_id: 13, timeout: 30, content: { query: 'SELECT 42 AS fixture_value FROM pg_catalog.pg_sleep(20)' }, inputs: { effective_parameters: {}, effective_inputs: {} } },
    metadata: state === 'success' ? { result: { columns: ['fixture_value'], rows_count: 1, summary: { preview_rows: [{ fixture_value: 42 }] } } } : {}, dev_task: { name: '导航查询' }, steps: [] }
  const previous = { ...execution, execution_id: old, status: 'success', attempt: 1 }
  const errors = []; page.on('pageerror', error => errors.push(error.message))
  const unexpected = await mockGraph(page, { permissions: ['develop.task.read', 'develop.data_read.execute', 'monitor.execution.read'], responses: {
    '/api/v1/develop/task-definitions': { items: [{ id: 37, name: '导航查询', dev_type: 'query', status: 'active', last_execution_id: id, last_execution_status: state, last_run_at: execution.created_at }], total: 1 },
    '/api/v1/develop/engines': { data: [] }, '/api/v1/monitor/task-providers': [],
    '/api/v1/monitor/executions': { executions: [execution, previous], total: 2, page: 1, page_size: 20 },
    [`/api/v1/monitor/executions/by-execution-id/${id}/tree`]: { execution, children: [], truncated: false },
    [`/api/v1/monitor/executions/by-execution-id/${id}/events`]: { items: [], has_more: false, next_cursor: 0 },
    [`/api/v1/develop/executions/${id}`]: execution, [`/api/v1/develop/executions/${id}/logs`]: []
  } })
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  let commands = 0
  page.on('request', request => { if (new URL(request.url()).pathname.startsWith('/api/v1/develop/') && request.method() !== 'GET') commands++ })
  await assertExecutionNavigationPage({ page, expect, task: 37, id, old, state, engine: 13, deadline: performance.now() + 15000 })
  expect(commands).toBe(0); expect(unexpected).toEqual([]); expect(errors).toEqual([])
})


async function prepareDifferenceLogPages(page) {
  const responses = {
    '/api/v1/monitor/task-providers': [],
    '/api/v1/monitor/executions': { executions: [], total: 0, page: 1, page_size: 20 }
  }
  const samples = []
  for (let index = 0; index < 5; index++) {
    const uuid = id.slice(0, -1) + (index + 1)
    const status = index === 0 ? 'success' : 'failed'
    const execution = { id: 1500 + index, execution_id: uuid, module: 'develop', task_type: 'workflow',
      source: 'develop', status, attempt: 1, created_at: '2026-11-17T00:00:00Z', steps: [] }
    const timestamp = '2026-11-17T00:00:00Z'
    const logs = [{ event: 'workflow_started', level: 'INFO', timestamp },
      { event: 'task_progress', level: 'INFO', progress: 0, task_id: 'load_a', timestamp },
      { event: status === 'success' ? 'workflow_succeeded' : 'workflow_failed', level: status === 'success' ? 'INFO' : 'ERROR', timestamp }]
    responses['/api/v1/monitor/executions/by-execution-id/' + uuid + '/tree'] = { execution, children: [], truncated: false }
    responses['/api/v1/monitor/executions/by-execution-id/' + uuid + '/events'] = { items: [], has_more: false, next_cursor: 0 }
    responses['/api/v1/develop/executions/' + uuid] = execution
    responses['/api/v1/develop/executions/' + uuid + '/logs'] = { execution_id: uuid, logs }
    samples.push({ execution_id: uuid, event_count: logs.length, sha256: createHash('sha256').update(JSON.stringify(logs)).digest('hex') })
  }
  await mockGraph(page, { permissions: ['develop.task.read', 'develop.data_read.execute', 'monitor.execution.read'], responses })
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  await page.goto('/monitor/executions?execution_id=' + id)
  return samples
}

test('geometry difference log oracle reads the five execution pages', async ({ page }) => {
  // Fixture setup precedes the verifier's own 60-second deadline for all ten reads.
  test.setTimeout(90_000)
  const samples = await prepareDifferenceLogPages(page)
  const report = { reads: 0, samples: [] }
  const stages = []
  await verifyDifferenceLogPages(page, samples, report, stage => stages.push(stage))
  expect(report.reads).toBe(10)
  expect(report.samples.map(r => r.execution_id)).toEqual(samples.map(r => r.execution_id))
  expect(stages.at(-1)).toBe('execution_logs_verified')
})

test('geometry difference log oracle refuses an unavailable response', async ({ page }) => {
  const samples = await prepareDifferenceLogPages(page)
  await page.route('**/api/v1/develop/executions/*/logs', route =>
    route.fulfill({ status: 503, json: { error_code: 'execution_logs_unavailable', error: 'private synthetic detail' } }))
  const failure = { reads: 0, samples: [] }
  const stages = []
  await expect(verifyDifferenceLogPages(page, samples, failure, stage => stages.push(stage))).rejects.toThrow('execution_logs_http_failed')
  expect(failure).toMatchObject({ reads: 1, samples: [],
    first_failure: { stage: 'enter', sample: 0, http_status: 503, code: 'execution_logs_http_failed' } })
  expect(stages.at(-1)).toBe('execution_logs_failed')
  expect(JSON.stringify(failure)).not.toContain('private synthetic detail')
})

const cleanupRequest = '550e8400-e29b-41d4-a716-446655440002'
const cleanupChild = '550e8400-e29b-41d4-a716-446655440003'
const cleanupSibling = '550e8400-e29b-41d4-a716-446655440004'
const cleanupHistoryPath = '/api/v1/system/admin/cleanup/history'
const cleanupDetailPath = '/api/v1/system/admin/cleanup/tasks/' + cleanupRequest
const professionalPath = '/api/v1/transfer/cleanup-executions/' + cleanupChild + '/result'

async function cleanupFixture(page, { selected, professional = false, override = {} }) {
  const task = { task_id: cleanupRequest, execution_id: id }
  const child = { execution_id: cleanupChild, parent_execution_id: id, module: 'transfer', task_type: 'cleanup_executor',
    source: 'system', attempt: 1, status: 'success', progress: 100 }
  const receipt = { schema: 'addp.cleanup-receipt/v1', execution_id: cleanupChild, parent_execution_id: id,
    request_id: cleanupRequest, module: 'transfer', attempt: 1, status: 'success', action: 'scan',
    summary: { scanned_items: 17 }, impact: {}, request_digest: 'a'.repeat(64), impact_digest: 'b'.repeat(64) }
  const sibling = { ...child, execution_id: cleanupSibling, module: 'meta', status: 'pending', progress: 0, attempt: 0 }
  const parent = { execution_id: id, source: 'system', module: 'system', task_type: 'cleanup', status: 'running' }
  const execution = selected === id ? parent : child
  const historyQueries = [], professionalReads = []
  page.on('request', req => {
    const url = new URL(req.url())
    if (url.pathname === cleanupHistoryPath) historyQueries.push(Object.fromEntries(url.searchParams))
    if (url.pathname === professionalPath) professionalReads.push(req.method())
  })
  const unexpected = await mockGraph(page, {
    permissions: ['monitor.execution.read', 'system.cleanup.read', ...(professional ? ['transfer.cleanup_result.read'] : [])],
    responses: {
      '/api/v1/monitor/task-providers': [], '/api/v1/monitor/executions': { executions: [execution], total: 1, page: 1, page_size: 20 },
      [`/api/v1/monitor/executions/by-execution-id/${selected}/tree`]: { execution, children: [], truncated: false },
      [`/api/v1/monitor/executions/by-execution-id/${selected}/events`]: { items: [], has_more: false, next_cursor: 0 },
      [cleanupHistoryPath]: { total: 1, tasks: [task] },
      [cleanupDetailPath]: { task_id: cleanupRequest, task, action: 'scan', status: 'running', module_executions: [{ ...child, receipt }, sibling] },
      [professionalPath]: { schema: 'addp.cleanup-professional-result/v1', execution_id: cleanupChild, request_id: cleanupRequest,
        module: 'transfer', attempt: 1, status: 'success', action: 'scan', summary: receipt.summary, statistics: { transferred_items: 321 } },
      ...override
    }
  })
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  return { unexpected, professionalReads, historyQueries }
}

for (const [selected, professional] of [[id, false], [cleanupChild, false], [cleanupChild, true]]) {
  test(`cleanup result and exact return for ${selected === id ? 'parent' : 'child'}, professional read ${professional}`, async ({ page }) => {
    const { unexpected, professionalReads, historyQueries } = await cleanupFixture(page, { selected, professional })
    const monitorURL = '/monitor/executions?source=system&execution_id=' + selected
    await page.goto(monitorURL)
    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    await frame.getByRole('dialog').getByRole('button', { name: '查看执行结果' }).click()
    const target = '/system/executions/' + id + (selected === id ? '' : '/children/' + selected)
    await expect(page).toHaveURL(url => url.pathname === target && url.searchParams.get('return_to') === monitorURL)
    await expect(frame.getByText(cleanupRequest, { exact: true })).toBeVisible()
    const row = frame.locator('.cleanup-module-results article').filter({ hasText: cleanupChild })
    await expect(row.getByRole('heading', { name: 'transfer · 成功' })).toBeVisible()
    await expect(row.getByText('标准回执', { exact: true }).locator('+ dd')).toHaveText('成功')
    await expect(row.getByText('17', { exact: true })).toBeVisible()
    await expect(frame.locator('.cleanup-module-results article').filter({ hasText: cleanupSibling })).toHaveCount(selected === id ? 1 : 0)
    expect(professionalReads).toEqual([])
    const button = row.getByRole('button', { name: '模块专业结果' })
    if (professional) {
      await button.click()
      await expect(row.getByText('321', { exact: true })).toBeVisible()
      expect(professionalReads).toEqual(['GET'])
    } else {
      await expect(button).toHaveCount(0)
    }
    expect(historyQueries).toEqual([{ parent_execution_id: id, page: '1', page_size: '1' }])
    await frame.getByRole('button', { name: '返回本次执行' }).click()
    await expect(page).toHaveURL(monitorURL)
    await expect(frame.getByRole('dialog')).toContainText(selected)
    expect(unexpected).toEqual([])
  })
}

for (const [scenario, override] of [
  ['non-unique history', { [cleanupHistoryPath]: { total: 2, tasks: [{ task_id: cleanupRequest, execution_id: id }] } }],
  ['different parent', { [cleanupDetailPath]: { task_id: cleanupRequest, task: { execution_id: cleanupSibling } } }],
  ['invisible child', { [cleanupDetailPath]: { task_id: cleanupRequest, task: { execution_id: id }, action: 'scan', status: 'success', module_executions: [] } }]
]) test('cleanup rejects ' + scenario, async ({ page }) => {
  const { professionalReads, unexpected } = await cleanupFixture(page, { selected: cleanupChild, professional: true, override })
  await page.goto('/system/executions/' + id + '/children/' + cleanupChild)
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(frame.getByText('无法读取本次执行结果')).toBeVisible()
  await expect(frame.locator('.cleanup-module-results')).toHaveCount(0)
  expect(professionalReads).toEqual([])
  expect(unexpected).toEqual([])
})

const discoveryObservation = {
  execution_id: id, module: 'security', task_type: 'sensitive_data_discovery', source: 'security', status: 'success'
}
const observationPath = '/api/v1/monitor/executions/by-execution-id/' + id
const findingsPath = '/api/v1/security/findings'
const discoveryMonitor = '/monitor/executions?module=security&execution_id=' + id
const finding = { id: 'older-finding', discovery_execution_id: id, component_key: 'column:order_code',
  detector_code: 'pattern', detector_version: 'v2', confidence: 0.91, observed_at: '2026-11-17T00:00:00Z',
  explanation: { governance_source: 'later-review' } }

async function discoveryFixture(page, { permissions = ['security.finding.read'], execution = discoveryObservation, data = [finding] } = {}) {
  const queries = []
  page.on('request', req => {
    const url = new URL(req.url())
    if (url.pathname === findingsPath) queries.push(Object.fromEntries(url.searchParams))
  })
  const unexpected = await mockGraph(page, { permissions: ['monitor.execution.read', 'security.enrollment.read', ...permissions], responses: {
    '/api/v1/monitor/task-providers': [],
    '/api/v1/monitor/executions': { executions: [discoveryObservation], page: 1, page_size: 20, total: 1 },
    [observationPath + '/tree']: { execution: discoveryObservation, children: [], truncated: false },
    [observationPath + '/events']: { items: [], has_more: false, next_cursor: 0 },
    [observationPath]: execution,
    [findingsPath]: { data, total: data.length, page: 1, page_size: 20, total_pages: data.length ? 1 : 0 }
  } })
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  return { frame: page.frameLocator('iframe[data-testid="module-iframe"]'), queries, unexpected }
}

test('security discovery reads historical observations by execution and returns to the same dialog', async ({ page }) => {
  const { frame, queries, unexpected } = await discoveryFixture(page)
  await page.goto(discoveryMonitor)
  await frame.getByRole('dialog').getByRole('button', { name: '查看执行结果' }).click()
  await expect(page).toHaveURL(url => url.pathname === '/security/executions/' + id && url.searchParams.get('return_to') === discoveryMonitor)
  await expect(frame.getByRole('cell', { name: 'column:order_code' })).toBeVisible()
  await expect(frame.getByText('later-review')).toHaveCount(0)
  expect(queries).toEqual([{ discovery_execution_id: id, page: '1', page_size: '20' }])
  await frame.getByRole('button', { name: '返回本次执行' }).click()
  await expect(page).toHaveURL(discoveryMonitor)
  await expect(frame.getByRole('dialog')).toContainText(id)
  expect(unexpected).toEqual([])
})

test('security discovery hides the result without finding read', async ({ page }) => {
  const { frame, queries } = await discoveryFixture(page, { permissions: [] })
  await page.goto(discoveryMonitor)
  await expect(frame.getByRole('dialog')).toContainText(id)
  await expect(frame.getByRole('button', { name: '查看执行结果' })).toHaveCount(0)
  expect(queries).toEqual([])
})

for (const [scenario, execution, data, expected] of [
  ['zero hits', { ...discoveryObservation, metadata: { finding_count: 0 } }, [], '本次发现成功，命中 0 项'],
  ['missing historical details', { ...discoveryObservation, metadata: { finding_count: 2 } }, [], '本次执行暂无已记录的发现'],
  ['unfinished', { ...discoveryObservation, status: 'running' }, [], '本次执行暂无已记录的发现'],
  ['wrong execution type', { ...discoveryObservation, task_type: 'cleanup_executor' }, [], '无法读取本次执行结果'],
  ['foreign finding', discoveryObservation, [{ ...finding, discovery_execution_id: cleanupSibling }], '无法读取本次执行结果']
]) test('security discovery distinguishes ' + scenario, async ({ page }) => {
  const { frame, queries, unexpected } = await discoveryFixture(page, { execution, data })
  await page.goto('/security/executions/' + id)
  await expect(frame.getByText(expected, { exact: true })).toBeVisible()
  if (scenario === 'wrong execution type') expect(queries).toEqual([])
  if (scenario === 'foreign finding') await expect(frame.getByText('column:order_code')).toHaveCount(0)
  expect(unexpected).toEqual([])
})

test('security discovery keeps the execution filter on page two and clears rows on a failed refresh', async ({ page }) => {
  const { frame, queries } = await discoveryFixture(page)
  await page.route('**' + findingsPath + '?**', route => {
    const n = Number(new URL(route.request().url()).searchParams.get('page'))
    const data = Array.from({ length: n === 1 ? 20 : 1 }, (_, i) => ({
      ...finding, id: `finding-${n}-${i}`, component_key: `page-${n}-column-${i}`
    }))
    return route.fulfill({ json: { data, page: n, page_size: 20, total: 21, total_pages: 2 } })
  })
  await page.goto('/security/executions/' + id)
  await frame.locator('.el-pagination .btn-next').click()
  await expect(frame.getByRole('cell', { name: 'page-2-column-0', exact: true })).toBeVisible()
  expect(queries.map(q => [q.discovery_execution_id, q.page])).toEqual([[id, '1'], [id, '2']])
  await page.route('**' + findingsPath + '?**', route => route.fulfill({ status: 503, json: { error: 'unavailable' } }))
  await frame.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(frame.getByText('无法读取本次执行结果')).toBeVisible()
  await expect(frame.getByText('page-2-column-0')).toHaveCount(0)
})
