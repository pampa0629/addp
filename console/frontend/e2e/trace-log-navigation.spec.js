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

test('Console row detail preserves the upper filters and instance context', async ({ page }) => {
  const state = await fixture(page)
  const query = new URLSearchParams(traceQuery())
  query.delete('trace_id')
  await page.goto('/monitor/traces?' + query)
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  const instances = frame.getByTestId('trace-instances')
  const results = frame.getByTestId('trace-results')
  await expect(results).toContainText(traceID)
  const table = await instances.elementHandle()
  const filters = await frame.getByTestId('trace-search').innerText()
  const rows = await instances.innerText()
  const reads = state.instanceCalls.length
  await results.getByRole('button', { name: '查看调用链' }).click()
  await expect(frame.getByTestId('trace-spans')).toContainText('/items/:id')
  await expect(page).toHaveURL(url => url.searchParams.get('trace_id') === traceID && url.searchParams.get('start') === query.get('start') && url.searchParams.get('end') === query.get('end'))
  expect(await table.evaluate(node => node.isConnected)).toBe(true)
  expect(await instances.innerText()).toBe(rows)
  expect(await frame.getByTestId('trace-search').innerText()).toBe(filters)
  await expect(frame.getByRole('textbox', { name: 'Trace ID', exact: true })).toHaveValue('')
  await expect(frame.locator('header').getByRole('button', { name: '返回实例列表', exact: true })).toBeVisible()
  expect(state.instanceCalls).toHaveLength(reads)
  await frame.getByTestId('trace-detail-navigation').getByRole('button').click()
  await expect(results).toContainText(traceID)
  expect(await instances.innerText()).toBe(rows)
  await results.getByRole('button', { name: '查看调用链' }).click()
  await expect(frame.getByTestId('trace-spans')).toContainText('/items/:id')
  await page.goBack()
  await expect(results).toContainText(traceID)
  await page.goForward()
  await expect(frame.getByTestId('trace-spans')).toContainText('/items/:id')
  await page.reload()
  await expect(frame.getByTestId('trace-spans')).toContainText('/items/:id')
  await expect(instances).toContainText('one')
  await expect(frame.getByRole('textbox', { name: 'Trace ID', exact: true })).toHaveValue('')
  await expect(page.locator('iframe[data-testid="module-iframe"]')).toHaveCount(1)
})

test('Trace instance pagination changes rows and restores the requested page in Console', async ({ page }) => {
  await fixture(page)
  const calls = []
  await page.route('**/api/v1/system/platform/module-instances?**', route => {
    const params = new URL(route.request().url()).searchParams
    const current = Number(params.get('page'))
    calls.push(current)
    const start = (current - 1) * 10
    const data = Array.from({ length: Math.min(10, 25 - start) }, (_, index) => ({
      id: start + index + 1, module_name: 'monitor', instance_id: 'instance-' + (start + index + 1),
      role: 'backend', status: 'down', node_id: '', process_metrics_declared: false, process_started_at: '2026-11-10T00:00:00Z'
    }))
    return route.fulfill({ json: { data, total: 25, page: current, page_size: 10 } })
  })
  await page.goto('/monitor/traces')
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  const instances = frame.getByTestId('trace-instances')
  await expect(instances.getByRole('button', { name: 'instance-1', exact: true })).toBeVisible()
  await frame.getByLabel('第 3 页', { exact: true }).click()
  await expect(page).toHaveURL(url => url.searchParams.get('page') === '3')
  await expect(instances.getByRole('button', { name: 'instance-21', exact: true })).toBeVisible()
  await expect(instances.getByRole('button', { name: 'instance-1', exact: true })).toHaveCount(0)
  await frame.getByRole('button', { name: '上一页', exact: true }).click()
  await expect(page).toHaveURL(url => url.searchParams.get('page') === '2')
  await expect(instances.getByRole('button', { name: 'instance-11', exact: true })).toBeVisible()
  await page.reload()
  await expect(page).toHaveURL(url => url.searchParams.get('page') === '2')
  await expect(instances.getByRole('button', { name: 'instance-11', exact: true })).toBeVisible()
  expect(calls).toEqual([1, 3, 2, 2])
})

test('Tenant permission strings cannot mount a platform Trace iframe', async ({ page }) => {
  const state = await tracesBackend(page,{identity:identity(tracePermissions,{type:'tenant',tenant_id:1})})
  await page.goto('/monitor/traces?'+traceQuery());await expect(page.locator('iframe[data-testid="module-iframe"]')).toHaveCount(0);expect(state.calls).toEqual([])
})

test('Console checks only the visible Trace page and opens its frozen sample window', async ({ page }) => {
  const state = await fixture(page)
  await page.route('**/api/v1/system/platform/module-instances?**', route => {
    const params = new URL(route.request().url()).searchParams
    const requested = params.get('ids')
    const current = Number(params.get('page') || 1)
    const ids = requested ? [Number(requested)] : Array.from({ length: 10 }, (_, i) => (current - 1) * 10 + i + 1)
    return route.fulfill({ json: { data: ids.map(id => ({
      id, module_name: 'monitor', instance_id: id === 1 ? 'one' : 'instance-' + id,
      role: 'backend', status: 'down', node_id: '', process_metrics_declared: false,
      process_started_at: '2026-11-10T00:00:00Z'
    })), total: requested ? 1 : 20, page: current, page_size: 10 } })
  })
  await page.goto('/monitor/traces')
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(frame.getByTestId('trace-sample-state').filter({ hasText: '有样本' })).toHaveCount(10)
  expect(state.sampleCalls.map(call => call.instance_ids)).toEqual(['1,2,3,4,5,6,7,8,9,10'])
  await frame.getByLabel('第 2 页', { exact: true }).click()
  await expect(frame.getByTestId('trace-instances')).toContainText('instance-11')
  await expect.poll(() => state.sampleCalls.length).toBe(2)
  expect(state.sampleCalls[1].instance_ids).toBe('11,12,13,14,15,16,17,18,19,20')
  await frame.getByLabel('第 1 页', { exact: true }).click()
  await expect.poll(() => state.sampleCalls.length).toBe(3)
  const window = state.sampleCalls[2]
  await frame.getByRole('button', { name: 'one', exact: true }).click()
  await expect(frame.getByTestId('trace-results')).toContainText('1'.repeat(32))
  expect(state.calls.at(-1).start).toBe(window.start)
  expect(state.calls.at(-1).end).toBe(window.end)
  await expect(page).toHaveURL(url => url.searchParams.get('start') === window.start && url.searchParams.get('end') === window.end && !url.searchParams.has('range'))
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
