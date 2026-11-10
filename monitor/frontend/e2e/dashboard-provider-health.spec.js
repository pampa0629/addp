import { expect, test } from '@playwright/test'

async function openDashboard(page, { theme = 'light', trend = [] } = {}) {
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
    '/api/v1/monitor/executions/trend': trend,
    '/api/v1/monitor/executions/runtime-metrics': { groups: [] }
  }
  await page.addInitScript(mode => {
    localStorage.setItem('addp-lang', 'zh-cn')
    localStorage.setItem('theme-mode', mode)
  }, theme)
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname
    if (!Object.hasOwn(responses, path)) throw new Error(`Unexpected fixture API ${path}`)
    return route.fulfill({ json: responses[path] })
  })
  await page.setViewportSize({ width: 1000, height: 900 })
  await page.goto('/dashboard')
  return providers
}

test('provider readiness reports ready, partially ready and unavailable instances', async ({ page }) => {
  const providers = await openDashboard(page)
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

test('trend tooltip text stays readable when the theme changes', async ({ page }) => {
  await openDashboard(page, {
    theme: 'dark',
    trend: [{ date: '2026-11-04', success_count: 49, failed_count: 8, total: 57 }]
  })

  const chart = page.locator('.trend-chart')
  const canvas = chart.locator('canvas')
  await canvas.hover({ position: { x: (await canvas.boundingBox()).width / 2, y: 150 } })
  const tooltip = chart.locator(':scope > div').filter({ hasText: '2026-11-04' })
  await expect(tooltip).toBeVisible()

  const contrast = () => tooltip.evaluate(element => {
    const luminance = color => {
      const rgb = color.match(/\d+/g).slice(0, 3).map(value => Number(value) / 255)
      const [r, g, b] = rgb.map(value => value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4)
      return 0.2126 * r + 0.7152 * g + 0.0722 * b
    }
    const background = luminance(getComputedStyle(element).backgroundColor)
    const text = [...element.querySelectorAll('div, span')]
      .filter(node => [...node.childNodes].some(child => child.nodeType === Node.TEXT_NODE && child.textContent.trim()))
    return Math.min(...text.map(node => {
      const foreground = luminance(getComputedStyle(node).color)
      return (Math.max(foreground, background) + 0.05) / (Math.min(foreground, background) + 0.05)
    }))
  })

  await expect(tooltip.getByText('49', { exact: true })).toBeVisible()
  expect(await contrast()).toBeGreaterThanOrEqual(4.5)
  const darkBackground = await tooltip.evaluate(element => getComputedStyle(element).backgroundColor)
  // The Console theme listener applies the current theme class on the iframe root.
  await page.evaluate(() => document.documentElement.classList.remove('dark'))
  await expect(tooltip).not.toHaveCSS('background-color', darkBackground)
  await expect.poll(contrast).toBeGreaterThanOrEqual(4.5)
})
