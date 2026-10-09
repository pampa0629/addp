import { browserTestOrigin } from '../../../common-frontend/basic/src/utils/browserTestIsolation.mjs'
import { expect, test } from '@playwright/test'
import { mockModuleQueryAPI } from './module-query.fixture'

async function fixture(page, status = 'up') {
  await page.clock.install({ time: new Date('2026-10-01T12:00:00Z') })
  await mockModuleQueryAPI(page, { logPermission: true, now: () => Date.parse('2026-10-01T12:00:00Z') })
  const queries = []
  let failure = false, failureStatus = 502, failureText = '日志后端不可用', retention = false, more = false, empty = false, pending = null
  await page.route('**/api/v1/system/platform/modules/*/instances/*/logs?*', async route => {
    if (route.request().method() === 'OPTIONS') return route.fallback()
    const query = Object.fromEntries(new URL(route.request().url()).searchParams)
    queries.push(query)
    const held = pending; pending = null
    if (held) await held
    const headers = { 'access-control-allow-origin': route.request().headers().origin || browserTestOrigin('system'), 'access-control-allow-credentials': 'true' }
    if (failure) return route.fulfill({ status: failureStatus, headers, json: { error: failureText } })
    const entries = empty ? [] : [{ timestamp: '2026-10-01T11:59:00Z', entry_id: `entry-${queries.length}`, level: 'unknown', channel: 'stderr', message: '<img src=x onerror="window.logExecuted=true">'+query.keyword, stack: 'Traceback\nValueError: failure' }]
    return route.fulfill({ headers, json: { entries, returned: entries.length, queried_at: '2026-10-01T12:00:00Z', collection_state: 'unknown', has_more: more && query.cursor !== 'older-2', next_cursor: more ? (query.cursor ? 'older-2' : 'older-1') : '', from: query.from, to: query.to, outside_retention: retention } })
  })
  await page.goto(`/modules?tab=instances&module_name=manager&status=${status}&page=2`)
  await page.getByRole('button', { name: '查看日志', exact: true }).first().click()
  const drawer = page.getByRole('dialog')
  await expect(drawer.getByTestId('runtime-log-output')).toContainText('<img src=x')
  return { drawer, queries, fail: (status = 502, text = '日志后端不可用') => { failure = true; failureStatus = status; failureText = text }, recover: () => { failure = false }, retention: () => { retention = true }, limit: () => { more = true }, empty: () => { empty = true }, hold: promise => { pending = promise } }
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
  await expect(drawer.getByRole('button', { name: '更早一批' })).toBeEnabled()
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


test('historical batches pin the window, pause follow, preserve errors and return', async ({ page }) => {
  const state = await fixture(page)
  state.limit()
  const { drawer, queries } = state
  await drawer.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(drawer.getByRole('button', { name: '更早一批' })).toBeEnabled()
  await drawer.locator('.el-switch').click()
  await expect.poll(() => queries.length).toBeGreaterThan(2)
  await page.clock.pauseAt(await page.evaluate(() => Date.now() + 1000))
  const pinned = { ...queries.at(-1) }
  await drawer.getByRole('button', { name: '更早一批' }).click()
  await expect(drawer.getByText('第 2 批', { exact: true })).toBeVisible()
  expect(queries.at(-1).cursor).toBe('older-1')
  expect(queries.at(-1).from).toBe(pinned.from); expect(queries.at(-1).to).toBe(pinned.to)
  const historyCount = queries.length
  await page.clock.fastForward(10000)
  expect(queries).toHaveLength(historyCount)
  state.fail()
  const output = await drawer.getByTestId('runtime-log-output').innerText()
  await drawer.getByRole('button', { name: '更早一批' }).click()
  await expect(drawer.getByText('日志后端不可用', { exact: true })).toBeVisible()
  await expect(drawer.getByText('第 2 批', { exact: true })).toBeVisible()
  expect(await drawer.getByTestId('runtime-log-output').innerText()).toBe(output)
  state.recover()
  await drawer.getByRole('button', { name: '更早一批' }).click()
  await expect(drawer.getByText('第 3 批', { exact: true })).toBeVisible()
  await expect(drawer.getByText('本次查询未发现更早记录', { exact: true })).toBeVisible()
  await expect(drawer.getByRole('button', { name: '更早一批' })).toBeDisabled()
  await drawer.getByRole('button', { name: '返回上一批' }).click()
  await expect(drawer.getByText('第 2 批', { exact: true })).toBeVisible()
  expect(queries.at(-1).cursor).toBe('older-1')
  await drawer.getByRole('button', { name: '回到最新' }).click()
  await expect(drawer.getByText('第 1 批', { exact: true })).toBeVisible()
  expect(queries.at(-1).cursor).toBeUndefined()
  expect(Date.parse(queries.at(-1).to)).toBeGreaterThan(Date.parse(pinned.to))
})

test('filter change discards historical output and delayed pagination cannot overwrite it', async ({ page }) => {
  const state = await fixture(page)
  state.limit()
  const { drawer, queries } = state
  await drawer.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(drawer.getByRole('button', { name: '更早一批' })).toBeEnabled()
  let release
  state.hold(new Promise(resolve => { release = resolve }))
  const count = queries.length
  await drawer.getByRole('button', { name: '更早一批' }).click()
  await expect.poll(() => queries.length).toBe(count + 1)
  state.fail()
  await drawer.getByPlaceholder('输入关键字').fill('new-filter')
  await drawer.getByPlaceholder('输入关键字').press('Enter')
  await expect(drawer.getByText('日志后端不可用', { exact: true })).toBeVisible()
  await expect(drawer.locator('.log-entry')).toHaveCount(0)
  release()
  state.recover()
  await drawer.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(drawer.getByTestId('runtime-log-output')).toContainText('new-filter')
  await expect(drawer.getByText('第 1 批', { exact: true })).toBeVisible()
  expect(queries.at(-1).cursor).toBeUndefined()
})


test('dense timestamp failure preserves the readable batch and retention warning', async ({ page }) => {
  const state = await fixture(page)
  state.limit(); state.retention()
  await state.drawer.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(state.drawer.getByText('所选范围超出配置保留期，结果可能不完整', { exact: true })).toBeVisible()
  await expect(state.drawer.getByRole('button', { name: '更早一批' })).toBeEnabled()
  const output = await state.drawer.getByTestId('runtime-log-output').innerText()
  const text = '同一时间戳的日志超过安全查询上限，无法继续。'
  state.fail(422, text)
  await state.drawer.getByRole('button', { name: '更早一批' }).click()
  await expect(state.drawer.getByText(text, { exact: true })).toBeVisible()
  await expect(state.drawer.getByText('第 1 批', { exact: true })).toBeVisible()
  expect(await state.drawer.getByTestId('runtime-log-output').innerText()).toBe(output)
  await expect(state.drawer.getByText('本次查询未发现更早记录', { exact: true })).toHaveCount(0)
})
