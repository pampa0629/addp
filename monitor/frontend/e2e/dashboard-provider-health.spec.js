import { expect, test } from '@playwright/test'

async function openDashboard(page, { theme = 'light', trend = [], runtimes = [], metrics = [], health = true, locale = 'zh-cn', failures = new Set() } = {}) {
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
          permissions: ['monitor.statistics.read', ...(health ? ['monitor.health.read'] : [])]
        }]
      }
    },
    '/api/v1/monitor/providers/health': providers,
    '/api/v1/monitor/runtime-instances/health': runtimes,
    '/api/v1/monitor/executions/stats': {},
    '/api/v1/monitor/executions/trend': trend,
    '/api/v1/monitor/executions/runtime-metrics': { groups: metrics }
  }
  await page.addInitScript(({ mode, locale }) => {
    localStorage.setItem('addp-lang', locale)
    localStorage.setItem('theme-mode', mode)
  }, { mode: theme, locale })
  const calls = []
  await page.route('**/api/v1/**', route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    calls.push(path + url.search)
    if (!Object.hasOwn(responses, path)) throw new Error(`Unexpected fixture API ${path}`)
    return route.fulfill(failures.has(path) ? { status: 503, json: { error: 'unavailable' } } : { json: responses[path] })
  })
  await page.setViewportSize({ width: 1000, height: 900 })
  await page.goto('/dashboard')
  return { providers, calls, responses }
}

test('provider readiness reports ready, partially ready and unavailable instances', async ({ page }) => {
  const { providers } = await openDashboard(page)
  const cards = page.locator('.module-runtime-card')
  await expect(page.getByText('任务运行概况', { exact: true })).toBeVisible()
  await expect(cards).toHaveCount(providers.length)

  const develop = cards.filter({ hasText: 'develop' })
  await expect(develop.getByText('就绪', { exact: true })).toBeVisible()
  await expect(develop).toContainText('1/1')

  const graph = cards.filter({ hasText: 'graph' })
  await expect(graph).toContainText('不可用')
  await expect(graph).toContainText('0/1')

  const manager = cards.filter({ hasText: 'manager' })
  await expect(manager).toContainText('部分就绪')
  await expect(manager).toContainText('1/2')

  await expect(cards.filter({ hasText: 'meta' })).toContainText('未检查')
  await expect(cards.getByText(/任务发现|探测被拒绝/)).toHaveCount(0)

  const trend = page.locator('.trend-card')
  const providerPanel = page.locator('.runtime-overview')
  const chart = trend.locator('canvas')
  await expect(chart).toBeVisible()
  const trendBox = await trend.boundingBox()
  const providerBox = await providerPanel.boundingBox()
  expect(providerBox.x).toBe(trendBox.x)
  expect(providerBox.width).toBe(trendBox.width)
  expect(providerBox.y).toBeGreaterThanOrEqual(trendBox.y + trendBox.height)

  for (const card of await cards.all()) {
    const button = card.getByRole('button')
    if (await button.getAttribute('aria-expanded') === 'true') await button.click()
  }
  const cardColumns = () => cards.evaluateAll(elements => elements.map(card => Math.round(card.getBoundingClientRect().left)))
  expect(new Set(await cardColumns()).size).toBe(2)

  const desktopChartWidth = (await chart.boundingBox()).width
  await page.setViewportSize({ width: 390, height: 900 })
  expect(new Set(await cardColumns()).size).toBe(1)
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

function metric(overrides = {}) {
  return { module: 'manager', task_type: 'embedding', execution_boundary: 'bounded',
    pending_count: 0, running_count: 2, completed_count: 1, failed_count: 1, timeout_count: 0,
    throughput_per_hour: 0.04, failure_rate: 100, avg_queue_duration_ms: 6, p95_queue_duration_ms: 10,
    avg_execution_duration_ms: 2000, p95_execution_duration_ms: 3000,
    automatic_retry_count: 0, automatic_retry_rate: 0, user_retry_count: 0, user_retry_rate: 0,
    recovery_count: 0, recovery_rate: 0, ...overrides }
}

for (const locale of ['zh-cn', 'en']) test(`module cards combine independent health and weighted metrics (${locale})`, async ({ page }, testInfo) => {
  const { calls } = await openDashboard(page, { locale, theme: locale === 'zh-cn' ? 'dark' : 'light',
    runtimes: [{ module: 'manager', role: 'execution_supervisor', runtime_name: 'generation', status: 'up',
      healthy_instances: 1, known_instances: 9, active_count: 2, capacity: 20 },
      { module: 'monitor', role: 'dispatcher', runtime_name: 'webhook', status: 'up',
        healthy_instances: 1, known_instances: 1, active_count: 0, capacity: 1 }],
    metrics: [metric(), metric({ task_type: 'raster_cog_generation', completed_count: 9, failed_count: 0, failure_rate: 0, throughput_per_hour: 0.38 })]
  })
  const manager = page.locator('[data-module="manager"]')
  const summary = manager.locator('.module-summary')
  await expect(summary).toContainText('10.00%')
  await expect(summary).toContainText('0 / 4')
  await expect(summary).toContainText('2 / 20')
  await expect(manager.locator('.runtime-detail')).toHaveCount(3)
  await expect(manager.locator('.runtime-detail').first()).toContainText('9')
  await expect(page.locator('[data-module="monitor"]')).toHaveCount(1)
  const before = calls.length
  await manager.getByRole('button').click()
  await expect(manager.locator('.module-details')).toHaveCount(0)
  expect(calls.length).toBe(before)

  const healthCalls = calls.filter(path => path.includes('/health')).length
  const windowSelect = page.getByRole('combobox', { name: locale === 'en' ? 'Execution metrics window' : '执行指标统计窗口' })
  await windowSelect.focus()
  await windowSelect.press('Enter')
  await page.getByRole('option', { name: locale === 'en' ? 'Last 7 Days' : '最近 7 天' }).click()
  await expect.poll(() => calls.some(path => path.endsWith('runtime-metrics?duration=7d'))).toBe(true)
  expect(calls.filter(path => path.includes('/health')).length).toBe(healthCalls)
  await expect(manager.getByRole('button')).toHaveAttribute('aria-expanded', 'false')
  await page.getByRole('combobox', { name: locale === 'en' ? 'Execution metrics window' : '执行指标统计窗口' }).blur()
  await page.mouse.move(0, 0)
  await expect(page.getByRole('option', { name: locale === 'en' ? 'Last 7 Days' : '最近 7 天' })).toBeHidden()
  await page.locator('.runtime-overview').screenshot({ path: testInfo.outputPath(`module-cards-${locale}.png`) })
})

test('statistics-only cards do not request health and refresh failures clear old metrics', async ({ page }) => {
  const failures = new Set()
  const { calls } = await openDashboard(page, { health: false, metrics: [metric()], failures })
  const card = page.locator('[data-module="manager"]')
  await expect(card.locator('.module-summary')).toContainText('100.00%')
  expect(calls.some(path => path.includes('/health'))).toBe(false)
  await expect(card).not.toContainText('后端就绪')
  failures.add('/api/v1/monitor/executions/runtime-metrics')
  await page.locator('.runtime-overview').getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByRole('alert').filter({ hasText: '加载执行运行时指标失败' }).first()).toBeVisible()
  await expect(card).toHaveCount(0)
})
