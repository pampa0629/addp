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
