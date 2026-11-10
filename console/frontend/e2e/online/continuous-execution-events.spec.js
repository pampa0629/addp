import { expect, request, test } from '@playwright/test'
import { existsSync, readFileSync, renameSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { identity, json, login } from './transfer-browser-support.js'

// Only explicitly verified screenshots are public; failure artifacts stay private.
test.use({ screenshot: 'off' })

test('continuous execution timeline, recovery and revoked Owner access', async ({ browser }) => {
  if (process.env.ADDP_ONLINE_HOSTED !== '1' || process.env.ADDP_ONLINE_TEST !== '1') throw new Error('dedicated Hosted entry required')
  const phase = process.env.ADDP_CONTINUOUS_BROWSER_PHASE
  // The live page stays open while the existing fault step replaces the Worker.
  test.setTimeout(phase === 'live' ? 420_000 : 180_000)
  const ids = JSON.parse(process.env.ADDP_CONTINUOUS_BROWSER_EXECUTIONS)
  const runID = process.env.ADDP_ONLINE_TEST_RUN_ID
  const tenantID = process.env.ADDP_ONLINE_TEST_TENANT_ID
  const principalID = process.env.ADDP_CONTINUOUS_BROWSER_PRINCIPAL
  const control = process.env.ADDP_CONTINUOUS_BROWSER_CONTROL
  const artifacts = process.env.ADDP_ONLINE_ARTIFACT_DIR
  const credentials = [process.env.ADDP_CONTINUOUS_BROWSER_USERNAME, process.env.ADDP_CONTINUOUS_BROWSER_PASSWORD]
  if (!['live', 'recovery', 'revocation', 'owner_unavailable'].includes(phase) || !control || !artifacts || credentials.some(value => !value)) throw new Error('incomplete browser scope')
  const marker = { run_id: runID, phase, execution_id: ids[0] }
  const checks = []
  let stage = 'login'
  const contexts = []
  const report = result => ({ schema_version: 'addp.continuous-execution-browser/v1', phase,
    run_id: runID, tenant_id: tenantID, principal_id: principalID, execution_ids: ids, checks, result })
  const destination = resolve(artifacts, `continuous-browser-${phase}.json`)
  const route = id => id ? `/monitor/executions?execution_id=${encodeURIComponent(id)}` : '/monitor/executions'
  async function signedPage(id, owner = true) {
    const prefix = owner ? 'owner_login' : 'fresh_monitor_only_login'
    stage = prefix
    const context = await browser.newContext({ baseURL: process.env.CONSOLE_URL, locale: 'zh-CN' })
    contexts.push(context)
    const page = await context.newPage()
    await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
    const token = await login(page, ...credentials, route(id))
    const api = await request.newContext({ baseURL: process.env.GATEWAY_URL, extraHTTPHeaders: { Authorization: `Bearer ${token}` } })
    try {
      stage = prefix + '_auth_context'
      const current = identity(await json(await api.get('/api/v1/system/auth/context'), 'browser AuthContext'))
      stage = prefix + '_principalType'
      expect(current.principalType).toBe('user')
      stage = prefix + '_contextType'
      expect(current.contextType).toBe('tenant')
      stage = prefix + '_principalID'
      expect(current.principalID).toBe(principalID)
      stage = prefix + '_tenantID'
      expect(current.tenantID).toBe(tenantID)
      stage = prefix + '_monitor_permission'
      expect(current.permissions.has('monitor.execution.read')).toBe(true)
      stage = prefix + '_owner_permission'
      expect(current.permissions.has('transfer.task.read')).toBe(owner)
    } finally { await api.dispose() }
    return { page, frame: page.frameLocator('iframe[data-testid="module-iframe"]') }
  }
  async function timeline(frame, terminal) {
    const prefix = stage
    stage = prefix + '_detail'
    const detail = frame.locator('.execution-detail-content')
    await expect(detail).toBeVisible()
    stage = prefix + '_retention'
    const events = detail.locator('.execution-events')
    await expect(events).toContainText('过程事件保留 30 天')
    stage = prefix + '_started'
    await expect(events.locator('tbody tr').filter({ hasText: '开始执行' })).toHaveCount(1)
    stage = prefix + '_progress'
    await expect(events.locator('tbody tr').filter({ hasText: '进度汇总' }).first()).toBeVisible()
    stage = prefix + '_terminal'
    if (terminal) await expect(events.locator('tbody tr').filter({ hasText: terminal })).toHaveCount(1)
    stage = prefix + '_no_samples'
    await expect(detail).not.toContainText('business-value-sentinel')
    stage = prefix
    return { detail, events }
  }
  function signal(name) {
    const temporary = resolve(control, `${name}.tmp`)
    writeFileSync(temporary, JSON.stringify(marker))
    renameSync(temporary, resolve(control, `${name}.json`))
  }
  async function waitForSignal(name) {
    await expect.poll(() => {
      const file = resolve(control, `${name}.json`)
      return existsSync(file) ? JSON.parse(readFileSync(file, 'utf8')) : null
    }, { timeout: 100_000, intervals: [200] }).toEqual(marker)
  }
  async function release() {
    signal('ready')
    await waitForSignal('release')
  }
  async function unavailableResponse(response) {
    expect(response.status()).toBe(503)
    const body = await response.json()
    expect(Object.keys(body).sort()).toEqual(['error', 'error_code'])
    expect(body.error_code).toBe('execution_owner_unavailable')
    expect(typeof body.error).toBe('string')
    expect(JSON.stringify(body)).not.toContain('business-value-sentinel')
  }
  try {
    const { page, frame } = await signedPage(ids[0])
    if (phase === 'live') {
      stage = 'live_timeline'
      const { detail, events } = await timeline(frame)
      const rows = events.locator('tbody tr')
      const refresh = events.getByRole('button', { name: '刷新过程', exact: true })
      const eventPath = `/api/v1/monitor/executions/by-execution-id/${ids[0]}/events`
      function observePage(after, requireItems = false) {
        const observed = page.waitForResponse(async response => {
          const url = new URL(response.url())
          return response.request().method() === 'GET' && url.pathname === eventPath &&
            url.searchParams.get('after') === String(after) && response.ok() &&
            (!requireItems || (await response.json()).items.length > 0)
        }, { timeout: 120_000 }).then(response => response.json())
        observed.catch(() => {})
        return observed
      }
      async function refreshFromStart() {
        const observed = observePage(0)
        await refresh.click()
        const first = await observed
        expect(first.has_more).toBe(false)
        return first
      }
      function assertCursor(items, after = 0) {
        for (const item of items) {
          expect(item.execution_id).toBe(ids[0])
          expect(item.id).toBeGreaterThan(after)
          after = item.id
        }
      }

      await expect(events).toContainText('写入记录: 2')
      const initial = await refreshFromStart()
      assertCursor(initial.items)
      expect(initial.next_cursor).toBe(initial.items.at(-1).id)
      await expect(rows).toHaveCount(initial.items.length)
      checks.push('timeline')

      stage = 'live_automatic_progress'
      const next = observePage(initial.next_cursor, true)
      await release()
      const liveItems = [...initial.items]
      let additional = await next
      while (true) {
        assertCursor(additional.items, liveItems.at(-1).id)
        expect(additional.next_cursor).toBe(additional.items.at(-1).id)
        liveItems.push(...additional.items)
        if (additional.items.some(item => item.kind === 'progress' && item.counters.records_written === 4)) break
        additional = await observePage(additional.next_cursor, true)
      }
      await expect(events).toContainText('写入记录: 4')
      await expect(rows).toHaveCount(liveItems.length)
      checks.push('automatic_committed_progress')

      stage = 'live_refresh_cursor_consistency'
      const refreshed = await refreshFromStart()
      expect(refreshed.items).toEqual(liveItems)
      await expect(rows).toHaveCount(liveItems.length)
      await expect(rows.filter({ hasText: '开始执行' })).toHaveCount(1)
      checks.push('cursor_and_refresh_match')
      await page.screenshot({ path: resolve(artifacts, 'continuous-live-events.png'), fullPage: true })

      stage = 'live_same_page_terminal'
      signal('live_progress_observed')
      await expect(detail).toContainText('lease_expired', { timeout: 200_000 })
      await expect(rows.filter({ hasText: '执行失败' })).toHaveCount(1)
      await expect(rows.last()).toContainText('执行失败')
      await expect(rows).toHaveCount(liveItems.length + 1)
      await expect(detail).not.toContainText('business-value-sentinel')
      checks.push('same_page_terminal_convergence', 'no_samples')
    } else if (phase === 'recovery') {
      stage = 'old_failure'
      let visible = await timeline(frame, '执行失败')
      await expect(visible.detail).toContainText('lease_expired')
      checks.push('old_failure')
      stage = 'recovery_source'
      await page.goto(route(ids[1]))
      visible = await timeline(frame, '执行取消')
      await expect(visible.detail.locator('.metadata-summary')).toContainText(ids[0])
      await expect(visible.detail.locator('.metadata-summary')).toContainText('恢复来源执行 UUID')
      checks.push('recovery_source')
      await page.screenshot({ path: resolve(artifacts, 'continuous-recovery-source.png'), fullPage: true })
      stage = 'record_failure_code'
      await page.goto(route(ids[2]))
      visible = await timeline(frame, '执行失败')
      await expect(visible.detail).toContainText('missing_source_key')
      checks.push('record_failure_code', 'no_samples')
      await page.screenshot({ path: resolve(artifacts, 'continuous-record-failure.png'), fullPage: true })
    } else if (phase === 'owner_unavailable') {
      stage = 'ordinary_event_transport_failure'
      const visible = await timeline(frame, '执行失败')
      try {
        await page.context().setOffline(true)
        const failed = page.waitForEvent('requestfailed', { predicate: value =>
          value.method() === 'GET' && new URL(value.url()).pathname === `/api/v1/monitor/executions/by-execution-id/${ids[0]}/events` })
        await visible.events.getByRole('button', { name: '刷新过程', exact: true }).click()
        await failed
        await expect(visible.events.getByRole('alert')).toBeVisible()
        await expect(visible.events.locator('tbody tr')).toHaveCount(0)
        await expect(visible.detail).toBeVisible()
        await expect(visible.detail).toContainText(ids[0])
        await expect(frame.locator('.el-tree')).toBeVisible()
        checks.push('event_transport_failure_isolated')
      } finally {
        await page.context().setOffline(false)
      }
      await visible.events.getByRole('button', { name: '刷新过程', exact: true }).click()
      await expect(visible.events.locator('tbody tr').first()).toBeVisible()
      checks.push('event_transport_recovered')

      stage = 'owner_outage_previously_readable'
      const running = await signedPage(ids[1])
      await timeline(running.frame)
      await expect(running.frame.locator('.execution-detail-content')).toContainText(ids[1])
      await expect(running.frame.locator('.el-tree')).toBeVisible()
      const list = await signedPage()
      const rows = list.frame.locator('.execution-list .el-table__row')
      const total = list.frame.locator('.el-pagination__total')
      await expect(rows.first()).toBeVisible()
      await expect.poll(async () => Number((await total.innerText()).match(/\d+/)?.[0] || 0)).toBeGreaterThan(0)
      checks.unshift('previously_readable')

      // These responses come from real Monitor BFF authorization while the proven
      // Transfer process is SIGSTOP'ed externally. No HTTP response is mocked.
      const eventFailure = page.waitForResponse(response => response.status() === 503 &&
        new URL(response.url()).pathname === `/api/v1/monitor/executions/by-execution-id/${ids[0]}/events`)
      const treeFailure = running.page.waitForResponse(response => response.status() === 503 &&
        new URL(response.url()).pathname === `/api/v1/monitor/executions/by-execution-id/${ids[1]}/tree`)
      const listFailure = list.page.waitForResponse(response => response.status() === 503 &&
        new URL(response.url()).pathname === '/api/v1/monitor/executions')
      signal('ready')
      await waitForSignal('owner_stopped')
      stage = 'owner_outage_real_503'
      await Promise.all([
        visible.events.getByRole('button', { name: '刷新过程', exact: true }).click(),
        list.frame.getByRole('button', { name: '重置', exact: true }).click()
      ])
      for (const response of await Promise.all([eventFailure, treeFailure, listFailure])) await unavailableResponse(response)
      checks.push('real_owner_503')
      stage = 'owner_outage_old_views_cleared'
      for (const view of [frame, running.frame]) {
        await expect(view.locator('.execution-detail-content, .execution-steps, .execution-events, .el-tree')).toHaveCount(0)
        await expect(view.getByRole('button', { name: '重新加载', exact: true })).toBeVisible()
      }
      checks.push('old_details_tree_steps_events_cleared')
      await expect(rows).toHaveCount(0)
      await expect.poll(async () => Number((await total.innerText()).match(/\d+/)?.[0])).toBe(0)
      checks.push('old_list_and_total_cleared')
      await page.screenshot({ path: resolve(artifacts, 'continuous-owner-unavailable-detail.png'), fullPage: true })
      await list.page.screenshot({ path: resolve(artifacts, 'continuous-owner-unavailable-list.png'), fullPage: true })
      signal('owner_failure_observed')
      await waitForSignal('release')

      stage = 'owner_outage_recovered_authorized_reads'
      await Promise.all([
        frame.getByRole('button', { name: '重新加载', exact: true }).click(),
        running.frame.getByRole('button', { name: '重新加载', exact: true }).click(),
        list.frame.getByRole('button', { name: '重置', exact: true }).click()
      ])
      await timeline(frame, '执行失败')
      await timeline(running.frame)
      await expect(rows.first()).toBeVisible()
      await expect.poll(async () => Number((await total.innerText()).match(/\d+/)?.[0] || 0)).toBeGreaterThan(0)
      for (const view of [frame, running.frame]) await expect(view.locator('.execution-detail-content')).not.toContainText('business-value-sentinel')
      checks.push('recovered_authorized_reads', 'no_samples')
      await running.page.screenshot({ path: resolve(artifacts, 'continuous-owner-recovered.png'), fullPage: true })
    } else {
      stage = 'previously_readable'
      const visible = await timeline(frame, '执行失败')
      checks.push('previously_readable')
      await release()
      stage = 'old_content_cleared'
      // A real request using the revoked session must clear the displayed facts.
      await visible.events.getByRole('button', { name: '刷新过程', exact: true }).click()
      await expect.poll(async () => (await page.locator('iframe[data-testid="module-iframe"]').count())
        ? await frame.locator('.execution-events tbody tr').count() : 0).toBe(0)
      await expect.poll(async () => (await page.locator('iframe[data-testid="module-iframe"]').count())
        ? await frame.locator('.execution-detail-content').count() : 0).toBe(0)
      checks.push('old_content_cleared')
      stage = 'fresh_monitor_only_denied'
      const fresh = await signedPage(ids[0], false)
      stage = 'fresh_monitor_only_iframe'
      await expect(fresh.page.locator('iframe[data-testid="module-iframe"]')).toBeVisible()
      stage = 'fresh_monitor_only_detail_denied'
      const deniedTitle = fresh.frame.locator('.execution-detail-dialog .el-result__title')
      await expect(deniedTitle).toBeVisible()
      await expect(deniedTitle).toHaveText('未能加载目标执行详情')
      stage = 'fresh_monitor_only_no_detail'
      await expect(fresh.frame.locator('.execution-detail-content')).toHaveCount(0)
      stage = 'fresh_monitor_only_no_events'
      await expect(fresh.frame.locator('.execution-events')).toHaveCount(0)
      checks.push('fresh_monitor_only_denied', 'no_samples')
      await fresh.page.screenshot({ path: resolve(artifacts, 'continuous-revoked-reader.png'), fullPage: true })
    }
    writeFileSync(destination, JSON.stringify(report('passed')))
  } catch {
    writeFileSync(destination, JSON.stringify({ ...report('failed'), failed_stage: stage }))
    throw new Error(`continuous browser failed at ${stage}`)
  } finally {
    for (const context of contexts) await context.close()
  }
})
