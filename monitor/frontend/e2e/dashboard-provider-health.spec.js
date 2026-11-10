import { expect, test } from '@playwright/test'

test('provider readiness reports ready, partially ready and unavailable instances', async ({ page }) => {
  const ready = { status: 'up', module_health: { status: 'up' } }
  const unready = { status: 'down', module_health: { status: 'down' } }
  const providers = [
    { module: 'develop', status: 'up', backends: [ready] },
    { module: 'graph', status: 'down', backends: [unready] },
    { module: 'manager', status: 'degraded', backends: [ready, unready] },
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
  await expect(page.getByText('任务提供者就绪', { exact: true })).toBeVisible()
  await expect(cards).toHaveCount(providers.length)

  const develop = cards.filter({ hasText: 'develop' })
  await expect(develop.getByText('就绪', { exact: true })).toBeVisible()
  await expect(develop).toContainText('后端就绪：1/1')

  const graph = cards.filter({ hasText: 'graph' })
  await expect(graph).toContainText('不可用')
  await expect(graph).toContainText('后端就绪：0/1')

  const manager = cards.filter({ hasText: 'manager' })
  await expect(manager).toContainText('部分就绪')
  await expect(manager).toContainText('后端就绪：1/2')

  await expect(cards.filter({ hasText: 'meta' })).toContainText('后端就绪：未检查')
  await expect(cards.getByText(/任务发现|探测被拒绝/)).toHaveCount(0)
})
