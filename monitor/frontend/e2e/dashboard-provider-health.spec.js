import { expect, test } from '@playwright/test'

test('provider readiness reports ready, partially ready and unavailable instances', async ({ page }) => {
  const ready = { status: 'up', module_health: { status: 'up' } }
  const unready = { status: 'down', module_health: { status: 'down' } }
  const providers = [
    { module: 'develop', status: 'up', backends: [ready] },
    { module: 'graph', status: 'down', backends: [unready] },
    { module: 'manager', status: 'degraded', backends: [ready, unready] },
    { module: 'meta', status: 'down', backends: [] },
    ...['model', 'orchestrator', 'quality', 'transfer'].map(module => ({ module, status: 'up', backends: [ready] }))
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
  await page.setViewportSize({ width: 1000, height: 900 })
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

  const trend = page.locator('.trend-card')
  const providerPanel = page.locator('.provider-health-card')
  const chart = trend.locator('canvas')
  await expect(chart).toBeVisible()
  const trendBox = await trend.boundingBox()
  const providerBox = await providerPanel.boundingBox()
  expect(providerBox.x).toBe(trendBox.x)
  expect(providerBox.width).toBe(trendBox.width)
  expect(providerBox.y).toBeGreaterThanOrEqual(trendBox.y + trendBox.height)

  const gridRows = () => cards.evaluateAll(elements => elements.map(card => Math.round(card.getBoundingClientRect().top)))
  const desktopRows = await gridRows()
  expect(desktopRows.slice(0, 4)).toEqual(Array(4).fill(desktopRows[0]))
  expect(desktopRows.slice(4)).toEqual(Array(4).fill(desktopRows[4]))
  expect(desktopRows[4]).toBeGreaterThan(desktopRows[0])

  const desktopChartWidth = (await chart.boundingBox()).width
  await page.setViewportSize({ width: 390, height: 900 })
  expect(new Set(await gridRows()).size).toBe(providers.length)
  await expect.poll(async () => (await chart.boundingBox()).width).toBeLessThan(desktopChartWidth)
})
