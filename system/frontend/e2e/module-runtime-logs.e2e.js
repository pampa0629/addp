import { expect, test } from '@playwright/test'
import { mockModuleQueryAPI } from './module-query.fixture'

async function fixture(page, status = 'up') {
  await page.clock.install({ time: new Date('2026-10-01T12:00:00Z') })
  await mockModuleQueryAPI(page, { logPermission: true, now: () => Date.parse('2026-10-01T12:00:00Z') })
  const queries = []
  let failure = false, limited = false, empty = false, pending = null
  await page.route('**/api/v1/system/platform/modules/*/instances/*/logs?*', async route => {
    if (route.request().method() === 'OPTIONS') return route.fallback()
    const query = Object.fromEntries(new URL(route.request().url()).searchParams)
    queries.push(query)
    const held = pending; pending = null
    if (held) await held
    const headers = { 'access-control-allow-origin': route.request().headers().origin || 'http://127.0.0.1:4173', 'access-control-allow-credentials': 'true' }
    if (failure) return route.fulfill({ status: 502, headers, json: { error: '日志后端不可用' } })
    const entries = empty ? [] : [{ timestamp: '2026-10-01T11:59:00Z', entry_id: `entry-${queries.length}`, level: 'unknown', channel: 'stderr', message: '<img src=x onerror="window.logExecuted=true">'+query.keyword, stack: 'Traceback\nValueError: failure' }]
    return route.fulfill({ headers, json: { entries, returned: entries.length, queried_at: '2026-10-01T12:00:00Z', collection_state: 'unknown', limited, outside_retention: false } })
  })
  await page.goto(`/modules?tab=instances&module_name=manager&status=${status}&page=2`)
  await page.getByRole('button', { name: '查看日志', exact: true }).first().click()
  const drawer = page.getByRole('dialog')
  await expect(drawer.getByTestId('runtime-log-output')).toContainText('<img src=x')
  return { drawer, queries, fail: () => { failure = true }, recover: () => { failure = false }, limit: () => { limited = true }, empty: () => { empty = true }, hold: promise => { pending = promise } }
}

test('permission hides log entry without disturbing instance reads', async ({ page }) => {
  await mockModuleQueryAPI(page)
  await page.goto('/modules?tab=instances')
  await expect(page.getByText('page-1-0', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '查看日志', exact: true })).toHaveCount(0)
})

test('log filters auto query, debounce input, render text and preserve list state', async ({ page }) => {
  const { drawer, queries } = await fixture(page)
  const initialURL = page.url()
  expect(Date.parse(queries[0].to)-Date.parse(queries[0].from)).toBe(15*60*1000)
  await expect(drawer.locator('.log-entry img')).toHaveCount(0)
  expect(await page.evaluate(() => window.logExecuted)).toBeUndefined()
  await drawer.getByText('异常堆栈', { exact: true }).click()
  await expect(drawer.locator('details pre')).toContainText('ValueError: failure')
  await drawer.locator('.el-select').filter({ has: page.getByRole('combobox', { name: '日志级别', exact: true }) }).click()
  await page.getByRole('option', { name: 'ERROR', exact: true }).click()
  await expect.poll(() => queries.at(-1)?.level).toBe('error')
  const input = drawer.getByPlaceholder('输入关键字')
  await page.clock.pauseAt(await page.evaluate(() => Date.now() + 1000))
  await input.fill('search')
  const count = queries.length
  await page.clock.fastForward(299)
  expect(queries).toHaveLength(count)
  await page.clock.fastForward(1)
  await expect.poll(() => queries.at(-1)?.keyword).toBe('search')
  await drawer.getByRole('button', { name: '重置', exact: true }).click()
  await expect.poll(() => queries.at(-1)?.keyword).toBe('')
  await drawer.getByRole('button', { name: '关闭此对话框' }).click()
  await expect(page).toHaveURL(initialURL)
  const closed = queries.length
  await page.clock.fastForward(10000)
  expect(queries).toHaveLength(closed)
})

test('DOWN window, bounded output, failed refresh and follow recovery are explicit', async ({ page }) => {
  const state = await fixture(page, 'down')
  const { drawer, queries } = state
  expect(queries[0].from).toBe('2026-10-01T11:44:00.000Z')
  expect(Date.parse(queries[0].to)).toBeGreaterThanOrEqual(Date.parse('2026-10-01T12:00:00Z'))
  expect(Date.parse(queries[0].to)).toBeLessThan(Date.parse('2026-10-01T12:01:00Z'))
  state.limit()
  await drawer.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(drawer.getByText(/已达到展示上限/)).toBeVisible()
  state.fail()
  await drawer.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(drawer.getByText('日志后端不可用', { exact: true })).toBeVisible()
  await expect(drawer.getByText(/当前显示上次结果/)).toBeVisible()
  await expect(drawer.getByTestId('runtime-log-output')).toContainText('<img src=x')
  state.recover(); state.empty()
  await drawer.locator('.el-switch').click()
  await page.clock.fastForward(3000)
  await expect(drawer.getByText(/当前条件没有匹配日志/)).toBeVisible()
  await expect(drawer.getByText('日志后端不可用', { exact: true })).toHaveCount(0)
})

test('a delayed old response cannot replace newer log filters', async ({ page }) => {
  const state = await fixture(page)
  let release
  state.hold(new Promise(resolve => { release = resolve }))
  const initial = state.queries.length
  await state.drawer.getByRole('button', { name: '刷新', exact: true }).click()
  await expect.poll(() => state.queries.length).toBe(initial+1)
  await state.drawer.getByPlaceholder('输入关键字').fill('new-filter')
  await state.drawer.getByPlaceholder('输入关键字').press('Enter')
  await expect.poll(() => state.queries.at(-1)?.keyword).toBe('new-filter')
  await expect(state.drawer.getByTestId('runtime-log-output')).toContainText('new-filter')
  release()
  await page.clock.fastForward(1000)
  await expect(state.drawer.getByTestId('runtime-log-output')).toContainText('new-filter')
})
