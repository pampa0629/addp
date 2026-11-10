import { expect, test } from '@playwright/test'

test('provider health separates backend readiness from task discovery', async ({ page }) => {
  const providers = [
    {
      module: 'develop', status: 'down', message: 'all Backend instances are unhealthy',
      backends: [{
        status: 'down', module_health: { status: 'up' },
        task_discovery: [{ status: 'down', status_code: 403 }, { status: 'down', status_code: 403 }]
      }]
    },
    {
      module: 'graph', status: 'down',
      backends: [{ status: 'down', module_health: { status: 'down' }, task_discovery: null }]
    },
    {
      module: 'manager', status: 'up',
      backends: [{ status: 'up', module_health: { status: 'up' }, task_discovery: [{ status: 'up', status_code: 200 }] }]
    },
    { module: 'meta', status: 'down', backends: [] }
  ]
  const responses = {
    '/api/v1/system/refresh': { access_token: 'fixture-token', expires_in: 3600 },
    '/api/v1/system/users/me': { id: 9, username: 'fixture-user' },
    '/api/v1/system/auth/context': {
      context: { type: 'tenant', tenant_id: '7' },
      authorization: {
        role_assignments: [{
          scope: { type: 'tenant', tenant_id: '7' },
          permissions: ['monitor.statistics.read', 'monitor.health.read']
        }]
      }
    },
    '/api/v1/monitor/providers/health': providers,
    '/api/v1/monitor/runtime-instances/health': [],
    '/api/v1/monitor/executions/stats': {},
    '/api/v1/monitor/executions/trend': [],
    '/api/v1/monitor/executions/runtime-metrics': { groups: [] }
  }
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname
    if (!Object.hasOwn(responses, path)) throw new Error(`Unexpected fixture API ${path}`)
    return route.fulfill({ json: responses[path] })
  })
  await page.goto('/dashboard')

  const cards = page.locator('.module-status-badge')
  await expect(page.getByText('任务提供者健康', { exact: true })).toBeVisible()
  await expect(cards).toHaveCount(providers.length)

  const develop = cards.filter({ hasText: 'develop' })
  await expect(develop).toContainText('不可用')
  await expect(develop).toContainText('后端就绪：1/1')
  await expect(develop).toContainText('任务发现：0/2（2 项探测被拒绝）')
  await expect(develop).not.toContainText('all Backend instances are unhealthy')

  const graph = cards.filter({ hasText: 'graph' })
  await expect(graph).toContainText('后端就绪：0/1')
  await expect(graph).toContainText('任务发现：未检查')
  await expect(graph).not.toContainText('探测被拒绝')

  const manager = cards.filter({ hasText: 'manager' })
  await expect(manager).toContainText('正常')
  await expect(manager).toContainText('后端就绪：1/1')
  await expect(manager).toContainText('任务发现：1/1')

  await expect(cards.filter({ hasText: 'meta' })).toContainText('后端就绪：未检查')
  await expect(page.getByText('离线', { exact: true })).toHaveCount(0)
})
