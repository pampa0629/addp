import { test, expect, request } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { login, identity, json } from './transfer-browser-support.js'

test.use({ screenshot: 'off', trace: 'off', video: 'off' })
const pathFor = id => `/api/v1/monitor/executions/by-execution-id/${id}/events`
const route = id => `/monitor/executions?execution_id=${encodeURIComponent(id)}`
const kindLabels = { started: '开始执行', progress: '进度汇总', completed: '执行完成', truncated: '采集截断' }
const counterLabels = { records_read: '读取记录', batch_index: '批次' }
const warning = '事件已达到采集或展示上限，当前过程记录不完整。'

test('real Console event pagination, storage truncation and display cap', async ({ browser }) => {
  test.setTimeout(220_000)
  if (process.env.ADDP_ONLINE_HOSTED !== '1' || process.env.ADDP_ONLINE_TEST !== '1' || process.env.ONLINE_SUITE !== 'execution-events-pagination') throw new Error('event_pagination_browser_boundary')
  const input = JSON.parse(process.env.ADDP_EVENTS_PAGINATION_INPUT)
  if (input.execution_ids.length !== 2 || input.ids.quota.length !== 1000 || input.ids.history.length !== 1201) throw new Error('event_pagination_browser_input')
  const artifact = process.env.ADDP_ONLINE_ARTIFACT_DIR
  const checks = []
  const reportPath = resolve(artifact, 'execution-events-pagination-browser.json')
  let stage = 'login'
  const context = await browser.newContext({ baseURL: process.env.CONSOLE_URL, locale: 'zh-CN' })
  const page = await context.newPage()
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  let api
  const report = { schema_version: 'addp.execution-events-browser/v1', result: 'failed', checks, quota_rows: 0, history_rows: 0, refresh_rows: 0 }
  try {
    const token = await login(page, process.env.ADDP_ONLINE_TEST_USER_USERNAME, process.env.ADDP_ONLINE_TEST_USER_PASSWORD, route(input.execution_ids[0]))
    api = await request.newContext({ baseURL: process.env.GATEWAY_URL, extraHTTPHeaders: { Authorization: `Bearer ${token}` } })
    const actual = identity(await json(await api.get('/api/v1/system/auth/context'), 'browser identity'))
    expect(actual.principalID).toBe(input.principal_id)
    expect(actual.tenantID).toBe(input.tenant_id)
    expect(actual.principalType).toBe('user'); expect(actual.contextType).toBe('tenant')
    expect([...actual.permissions].sort()).toEqual(['meta.scan_task.read', 'monitor.execution.read'])
    checks.push('real_password_auth_and_owner_identity')
    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const detail = frame.locator('.execution-detail-content')
    const events = detail.locator('.execution-events')
    const rows = events.locator('tbody tr')
    async function inspectDOM(items) {
      const timestamps = await page.evaluate(values => values.map(value => new Date(value).toLocaleString()), items.map(item => item.occurred_at))
      const expected = items.map((item, i) => [timestamps[i], String(item.attempt), kindLabels[item.kind], item.step_id || '—',
        Object.entries(item.counters).map(([key, value]) => `${counterLabels[key]}: ${value}`).join(' ')])
      const actualRows = await rows.evaluateAll(elements => elements.map(row => [...row.querySelectorAll('td')].map(cell => cell.textContent.replace(/\s+/g, ' ').trim())))
      expect(actualRows).toEqual(expected)
      await expect(detail).not.toContainText(input.nonce)
    }
    async function loadAll(name, id, captureFirst) {
      const expectedIDs = input.ids[name]
      await expect(detail).toBeVisible()
      await expect(detail).toContainText(id)
      await expect(events).toContainText('过程事件保留 30 天')
      await expect(rows).toHaveCount(100)
      // First request can finish during identity verification. Re-read it through
      // the visible refresh control, then validate every real response and row.
      const first = page.waitForResponse(response => response.request().method() === 'GET' && new URL(response.url()).pathname === pathFor(id) && new URL(response.url()).searchParams.get('after') === '0')
      await events.getByRole('button', { name: '刷新过程', exact: true }).click()
      const firstResponse = await first
      expect(firstResponse.status()).toBe(200)
      let result = await firstResponse.json()
      expect(result.items.map(item => item.id)).toEqual(expectedIDs.slice(0, 100))
      const items = [...result.items]
      for (let n = 2; n <= 10; n++) {
        const cursor = result.next_cursor
        const loaded = page.waitForResponse(response => response.request().method() === 'GET' && new URL(response.url()).pathname === pathFor(id) && new URL(response.url()).searchParams.get('after') === String(cursor))
        await events.getByRole('button', { name: '加载后续事件', exact: true }).click()
        const response = await loaded
        expect(response.status()).toBe(200)
        result = await response.json()
        expect(result.items.map(item => item.id)).toEqual(expectedIDs.slice((n - 1) * 100, n * 100))
        expect(result.items.length).toBe(100)
        expect(result.next_cursor).toBe(expectedIDs[n * 100 - 1])
        expect(new URL(response.url()).searchParams.get('limit')).toBe('100')
        items.push(...result.items)
        await expect(rows).toHaveCount(n * 100)
      }
      await expect(rows).toHaveCount(1000)
      expect(new Set(items.map(item => item.id)).size).toBe(1000)
      await inspectDOM(items)
      await expect(events.getByRole('button', { name: '加载后续事件', exact: true })).toHaveCount(0)
      await expect(events.getByRole('status')).toHaveText(warning)
      expect(result.has_more).toBe(name === 'history')
      if (name === 'quota') {
        await expect(rows.filter({ hasText: '采集截断' })).toHaveCount(1)
        expect(items.at(-1).kind).toBe('truncated')
      } else {
        await expect(rows.filter({ hasText: '采集截断' })).toHaveCount(0)
        expect(items.every(item => item.kind !== 'truncated')).toBe(true)
        const tail = await json(await api.get(`${pathFor(id)}?after=${result.next_cursor}&limit=100`), 'real API tail')
        expect(tail.items.map(item => item.id)).toEqual(expectedIDs.slice(1000, 1100))
      }
      await events.locator('.event-scroll').evaluate(element => { element.scrollTop = element.scrollHeight })
      await events.getByRole('status').scrollIntoViewIfNeeded()
      await page.screenshot({ path: resolve(artifact, `execution-events-${name}.png`), fullPage: true })
      if (captureFirst) report.quota_rows = 1000
      else report.history_rows = 1000
    }
    stage = 'quota_pages'
    await loadAll('quota', input.execution_ids[0], true)
    checks.push('quota_ten_pages_and_storage_marker')
    stage = 'history_pages'
    await page.goto(route(input.execution_ids[1]))
    await loadAll('history', input.execution_ids[1], false)
    checks.push('history_display_limit_with_real_next_page', 'all_page_ids_and_dom_projection')
    stage = 'refresh_reset'
    await events.getByRole('button', { name: '刷新过程', exact: true }).click()
    await expect(rows).toHaveCount(100)
    await expect(events.getByRole('status')).toHaveCount(0)
    await expect(events.getByRole('button', { name: '加载后续事件', exact: true })).toBeVisible()
    stage = 'reload_reset'
    await page.reload()
    await expect(rows).toHaveCount(100)
    await expect(detail).toContainText(input.execution_ids[1])
    expect(new URL(page.url()).searchParams.get('execution_id')).toBe(input.execution_ids[1])
    report.refresh_rows = 100
    checks.push('refresh_and_reload_reset_first_page')
    stage = 'close_reopen'
    await frame.locator('.execution-detail-dialog .el-dialog__headerbtn').click()
    await expect(events).toHaveCount(0)
    await expect.poll(() => new URL(page.url()).searchParams.has('execution_id')).toBe(false)
    await page.goto(route(input.execution_ids[0]))
    await expect(rows).toHaveCount(100)
    await expect(detail).toContainText(input.execution_ids[0])
    await expect(events.getByRole('status')).toHaveCount(0)
    checks.push('close_and_reopen_reset_first_page', 'safe_diagnostics')
    report.result = 'passed'
  } catch {
    report.failed_stage = stage
    throw new Error(`event_pagination_browser_failed:${stage}`)
  } finally {
    writeFileSync(reportPath, JSON.stringify(report))
    if (api) await api.dispose()
    await context.close()
  }
})
