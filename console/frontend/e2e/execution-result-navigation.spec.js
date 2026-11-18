import { createHash } from 'node:crypto'
import { verifyDifferenceLogPages } from './online/difference-execution-logs.mjs'
import { expect, test } from '@playwright/test'
import { mockGraph } from './graph.fixture'

const id = '550e8400-e29b-41d4-a716-446655440001'
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


test('geometry difference log oracle reads the five execution pages and refuses an unavailable response', async ({ page }) => {
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
  const report = { reads: 0, samples: [] }
  const stages = []
  await verifyDifferenceLogPages(page, samples, report, stage => stages.push(stage))
  expect(report.reads).toBe(10)
  expect(report.samples.map(r => r.execution_id)).toEqual(samples.map(r => r.execution_id))
  expect(stages.at(-1)).toBe('execution_logs_verified')
  await page.route('**/api/v1/develop/executions/*/logs', route =>
    route.fulfill({ status: 503, json: { error_code: 'execution_logs_unavailable', error: 'private synthetic detail' } }))
  const failure = { reads: 0, samples: [] }
  await expect(verifyDifferenceLogPages(page, samples, failure, stage => stages.push(stage))).rejects.toThrow('execution_logs_http_failed')
  expect(failure).toMatchObject({ reads: 1, samples: [],
    first_failure: { stage: 'enter', sample: 0, http_status: 503, code: 'execution_logs_http_failed' } })
  expect(stages.at(-1)).toBe('execution_logs_failed')
  expect(JSON.stringify(failure)).not.toContain('private synthetic detail')
})
