import { test, expect } from '@playwright/test'
import { tracesBackend, tracePermissions, traceQuery } from '../../../monitor/frontend/e2e/traces.fixture'
import { identity } from '../../../monitor/frontend/e2e/monitoring-targets.fixture'
const traceID = '1'.repeat(32)
async function fixture(page) {
  const state = await tracesBackend(page, { identity: identity([...tracePermissions,'platform.module_log.read']), longSpan: true })
  await page.route('**/api/v1/system/platform/modules', route => route.fulfill({ json: { modules: [{ id: 1, module_name: 'monitor', route_prefix: '/monitor', enabled: true, version: 1, instances: [] }] } }))
  const logs = []
  await page.route('**/api/v1/system/platform/modules/*/instances/*/logs?**', async route => {
    const q = Object.fromEntries(new URL(route.request().url()).searchParams);logs.push(q)
    const timestamp = new Date((Date.parse(q.from)+Date.parse(q.to))/2).toISOString()
    await route.fulfill({ json: { entries: [{ entry_id: 'one:1', timestamp, level: 'info', channel: 'stdout', message: 'linked log evidence', trace_id: traceID, span_id: '2'.repeat(16) }], returned: 1, queried_at: new Date().toISOString(), collection_state: 'unknown', has_more: false, next_cursor: '', from: q.from, to: q.to, outside_retention: false } })
  })
  return { ...state, logs }
}
test('Console restores a Trace, opens exact System logs, and returns through one authorized module iframe', async ({ page }, info) => {
  const state = await fixture(page);await page.goto('/monitor/traces?'+traceQuery())
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(frame.getByTestId('trace-spans')).toContainText('/items/:id')
  const initial = page.url();await page.reload();await expect(frame.getByTestId('trace-spans')).toContainText('/items/:id');expect(page.url()).toBe(initial)
  await frame.getByTestId('trace-spans').getByRole('button',{name:'关联日志',exact:true}).first().click()
  await expect(page).toHaveURL(url => url.pathname === '/system/modules' && url.searchParams.get('trace_id') === traceID && url.searchParams.get('log_instance') === 'one')
  await expect(frame.getByTestId('runtime-log-output')).toContainText('linked log evidence')
  expect(state.logs.at(-1).trace_id).toBe(traceID)
  expect(Date.parse(state.logs.at(-1).to)-Date.parse(state.logs.at(-1).from)).toBeLessThanOrEqual(3600000)
  const logURL = page.url();await page.reload();await expect(frame.getByTestId('runtime-log-output')).toContainText('linked log evidence');expect(page.url()).toBe(logURL)
  await frame.getByRole('dialog').getByRole('button',{name:'查询调用链',exact:true}).click()
  await expect(page).toHaveURL(url => url.pathname === '/monitor/traces' && url.searchParams.get('trace_id') === traceID)
  await expect(frame.getByTestId('trace-spans')).toContainText('/items/:id')
  await expect(page.locator('iframe[data-testid="module-iframe"]')).toHaveCount(1)
  await page.screenshot({path:info.outputPath('trace-system-log-return.png'),fullPage:true})
})
test('Tenant permission strings cannot mount a platform Trace iframe', async ({ page }) => {
  const state = await tracesBackend(page,{identity:identity(tracePermissions,{type:'tenant',tenant_id:1})})
  await page.goto('/monitor/traces?'+traceQuery());await expect(page.locator('iframe[data-testid="module-iframe"]')).toHaveCount(0);expect(state.calls).toEqual([])
})

for (const width of [1280, 900]) {
  test(`Trace instance list has aligned page gutters and section spacing (${width}px)`, async ({ page }) => {
    await page.setViewportSize({ width, height: 800 })
    await page.addInitScript(() => localStorage.setItem('theme-mode', 'dark'))
    await fixture(page)
    const end = new Date(Date.now() - 60_000)
    const timeRange = new URLSearchParams({
      start: new Date(end.getTime() - 15 * 60_000).toISOString(),
      end: end.toISOString()
    })
    await page.goto('/monitor/traces?' + timeRange)

    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(frame.getByTestId('trace-instances')).toContainText('monitor')
    const layout = await frame.getByTestId('traces').evaluate(root => {
      const width = document.documentElement.clientWidth
      const boxes = [...root.children].map(element => element.getBoundingClientRect())
      return {
        width,
        scrollWidth: document.documentElement.scrollWidth,
        titleLeft: root.querySelector('h2').getBoundingClientRect().left,
        top: boxes[0].top,
        left: boxes.map(box => box.left),
        right: boxes.map(box => width - box.right),
        gaps: boxes.slice(1).map((box, index) => box.top - boxes[index].bottom)
      }
    })
    const inset = layout.width <= 700 ? 16 : 24
    expect(layout.titleLeft).toBeCloseTo(inset, 0)
    expect(layout.top).toBeCloseTo(inset, 0)
    expect(layout.left.every(left => Math.abs(left - inset) < 1)).toBe(true)
    expect(layout.right.every(right => Math.abs(right - inset) < 1)).toBe(true)
    expect(Math.min(...layout.gaps)).toBeGreaterThanOrEqual(16)
    expect(layout.scrollWidth).toBe(layout.width)

    const filters = frame.getByTestId('trace-search')
    const fields = [
      filters.getByRole('combobox', { name: '服务模块', exact: true }),
      filters.getByRole('combobox', { name: '开始时间', exact: true }),
      filters.getByText(/^查至/),
      filters.getByRole('textbox', { name: 'Trace ID', exact: true })
    ]
    for (const field of fields) await expect(field).toBeInViewport({ ratio: 1 })
    await expect(frame.getByRole('combobox', { name: '观测层' })).toHaveCount(0)
    const boxes = await Promise.all(fields.map(field => field.boundingBox()))
    for (let index = 1; index < boxes.length; index++) {
      const previous = boxes[index - 1]
      const current = boxes[index]
      const sameRow = Math.abs(previous.y + previous.height / 2 - current.y - current.height / 2) < 1
      if (sameRow) expect(current.x).toBeGreaterThanOrEqual(previous.x + previous.width)
      else expect(current.y).toBeGreaterThanOrEqual(previous.y + previous.height)
      if (width === 1280) expect(sameRow).toBe(true)
    }
  })
}

for (const theme of ['light', 'dark', 'blue', 'purple']) {
  test(`Trace headings use the current foreground on first load (${theme})`, async ({ page }) => {
    await page.addInitScript(mode => localStorage.setItem('theme-mode', mode), theme)
    await fixture(page)
    await page.goto('/monitor/traces?' + traceQuery())

    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    if (theme !== 'light') await expect(frame.locator('html')).toHaveClass(/\bdark\b/)
    else await expect(frame.locator('html')).not.toHaveClass(/\bdark\b/)
    if (theme === 'blue' || theme === 'purple') {
      await expect(frame.locator('html')).toHaveClass(new RegExp(`\\b${theme}\\b`))
    }

    const title = frame.getByRole('heading', { name: '调用链', exact: true })
    await expect(title).toBeInViewport({ ratio: 1 })
    for (const heading of [title, frame.getByRole('heading', { name: 'GET /items/:id', exact: true })]) {
      await expect(heading).toBeVisible()
      const { actual, expected } = await heading.evaluate(element => {
        const style = getComputedStyle(element)
        const themeColor = style.getPropertyValue('--addp-text-primary').trim()
        const context = document.createElement('canvas').getContext('2d')
        context.fillStyle = style.color
        const actual = context.fillStyle
        context.fillStyle = themeColor
        return { actual, expected: CSS.supports('color', themeColor) ? context.fillStyle : '' }
      })
      expect(actual).toBe(expected)
    }
  })
}
