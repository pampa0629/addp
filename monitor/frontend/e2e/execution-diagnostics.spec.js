import { expect, test } from '@playwright/test'

async function installBackend(page, { empty = false, unavailable = false } = {}) {
  const execution = { id: 1, execution_id: 'diagnostic-run', module: 'orchestrator', task_type: 'orchestration', source: 'orchestrator', source_task_id: 'deleted-task', source_task_name: '历史任务', status: 'failed', progress: 50, current_step: 'transform', metadata: {}, error_details: { code: 'STEP_FAILED', category: 'connection_failed' }, steps: empty ? [] : [{ id: 'read', status: 'success', started_at: '2026-10-01T01:00:00Z', duration: 120 }, { id: 'transform', status: 'failed', duration: 8, error_code: 'connection_failed' }], steps_truncated: false }
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname
    let body
    let status = 200
    if (path === '/api/v1/system/refresh') body = { access_token: 'fixture-token', expires_in: 3600 }
    else if (path === '/api/v1/system/users/me') body = { id: 9, username: 'fixture-user' }
    else if (path === '/api/v1/system/auth/context') body = { context: { type: 'tenant', tenant_id: '7' }, authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '7' }, permissions: ['monitor.execution.read'] }] } }
    else if (path === '/api/v1/monitor/task-providers') body = []
    else if (path.endsWith('/tree')) { body = { execution, children: [], truncated: !empty }; if (unavailable) { status = 503; body = { error_code: 'execution_owner_unavailable', error: '任务所属模块暂时无法完成读取授权' } } }
    else if (path === '/api/v1/monitor/executions') body = { executions: [execution], total: 1, page: 1, page_size: 20 }
    else throw new Error(`Unexpected fixture API ${path}`)
    await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
  })
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
