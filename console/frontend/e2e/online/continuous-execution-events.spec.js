import { expect, request, test } from '@playwright/test'
import { existsSync, readFileSync, renameSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { assertCDCTaskHistory, assertCDCTaskMonitoring, identity, json, login } from './transfer-browser-support.js'

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
  if (!['live', 'recovery', 'revocation', 'owner_unavailable', 'cdc_initial', 'cdc_growth', 'cdc_resumed', 'cdc_history','cdc_alert_open','cdc_alert_unknown','cdc_alert_resolved'].includes(phase) || !control || !artifacts || credentials.some(value => !value)) throw new Error('incomplete browser scope')
  const marker = { run_id: runID, phase, execution_id: ids[0] }
  const checks = []
  const alertBinding = phase.startsWith('cdc_alert_') ? JSON.parse(process.env.ADDP_CONTINUOUS_BROWSER_ALERTS) : null
  let alertAPI
  let stage = 'login'
  const contexts = []
  const report = result => ({ schema_version: 'addp.continuous-execution-browser/v1', phase,
    run_id: runID, tenant_id: tenantID, principal_id: principalID, execution_ids: ids, checks, result, ...(alertBinding ? { alert_binding: alertBinding } : {}) })
  const destination = resolve(artifacts, `continuous-browser-${phase}.json`)
  const route = id => id ? `/monitor/executions?execution_id=${encodeURIComponent(id)}` : '/monitor/executions'
  async function signedPage(id, owner = true, destination = route(id)) {
    const prefix = owner ? 'owner_login' : 'fresh_monitor_only_login'
    stage = prefix
    const context = await browser.newContext({ baseURL: process.env.CONSOLE_URL, locale: 'zh-CN' })
    contexts.push(context)
    const page = await context.newPage()
    await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
    if (phase === 'cdc_history') await page.addInitScript(() => performance.setResourceTimingBufferSize(5000))
    const token = await login(page, ...credentials, destination)
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
    } finally {
      if (alertBinding) alertAPI = api
      else await api.dispose()
    }
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
    const history = phase === 'cdc_history' ? JSON.parse(process.env.ADDP_CONTINUOUS_BROWSER_HISTORY) : null
    const { page, frame } = await signedPage(ids[0], true, history
      ? `/monitor/cdc-resources?view=tasks&task_id=${history.task_id}&refresh=off` : alertBinding ? '/monitor/alerts' : route(ids[0]))
    if (alertBinding) {
      const binding = alertBinding
      const names = { cdc_consumer_stalled:'CDC 目标消费停滞', cdc_capture_delayed:'CDC 捕获持续延迟', cdc_backlog_high:'CDC 积压持续偏高' }
      const statusNames = { open:'待处理', acknowledged:'已确认', resolved:'已恢复' }
      const incidentIDs = Object.values(binding.incidents)
      const dateText = value => page.evaluate(value => new Date(value).toLocaleString(), value)
      const alertPage = frame.locator('.alert-list')
      async function currentIncidents(status) {
        stage='cdc_alert_api_'+status
        const payload = await json(await alertAPI.get('/api/v1/monitor/alerts?module=transfer&page_size=100'), 'CDC alert API')
        expect(payload.total).toBeLessThanOrEqual(100)
        const selected = payload.data.filter(row => incidentIDs.includes(row.id))
        expect(selected).toHaveLength(3)
        for (const row of selected) {
          expect(row.id).toBe(binding.incidents[row.signal_code])
          expect(row.tenant_id).toBe(Number(tenantID))
          expect(row.module).toBe('transfer')
          expect(row.source_task_id).toBe(String(binding.task_id))
          expect(row.details.generation).toBe(String(binding.generation))
          expect(row.status).toBe(status)
          if (phase === 'cdc_alert_unknown') {
            expect(row.details.evaluation_state).toBe('unknown')
            expect(row.last_observed_at).toBe(binding.last_observed_at[row.signal_code])
          }
        }
        return selected
      }
      async function findAlertRow(id) {
        const refreshed=page.waitForResponse(response => response.url().includes('/api/v1/monitor/alerts?') && response.request().method()==='GET')
        await alertPage.getByRole('button',{name:'查询',exact:true}).click()
        expect((await refreshed).ok()).toBe(true)
        await expect(alertPage.locator('.el-loading-mask')).toHaveCount(0)
        for (let index=0;index<5;index++) {
          const row=alertPage.locator(`tbody tr:has(td:first-child .cell:text-is("${id}"))`)
          if (await row.count()) return row
          const next=alertPage.locator('.el-pagination .btn-next')
          if (await next.isDisabled()) break
          const response=page.waitForResponse(response => response.url().includes('/api/v1/monitor/alerts?') && response.request().method()==='GET')
          await next.click();expect((await response).ok()).toBe(true)
          await expect(alertPage.locator('.el-loading-mask')).toHaveCount(0)
        }
        throw new Error('bound CDC incident is absent from the visible pages')
      }
      async function compareRows(status) {
        const selected=await currentIncidents(status)
        for (const row of selected) {
          stage='cdc_alert_row_'+row.signal_code
          const visible=await findAlertRow(row.id)
          await expect(visible).toContainText(names[row.signal_code])
          await expect(visible.getByTestId('cdc-alert-reason')).toContainText(String(binding.generation))
          const cells=visible.locator('td .cell')
          await expect(cells.nth(2)).toHaveText(statusNames[status])
          await expect(cells.nth(3)).toHaveText('transfer')
          await expect(cells.nth(4)).toHaveText(String(binding.task_id))
          await expect(cells.nth(5)).toHaveText(await dateText(row.opened_at))
          if (phase!=='cdc_alert_open') await expect(cells.nth(6)).toHaveText(await dateText(row.last_observed_at))
          if (phase==='cdc_alert_unknown') await expect(visible.getByTestId('cdc-performance-evaluation')).toContainText('观测未知')
          if (status!=='open') await expect(visible.getByRole('button',{name:'确认',exact:true})).toHaveCount(0)
        }
        return selected
      }
      stage='cdc_alert_identity'
      await expect(alertPage).toBeVisible()
      checks.push('alert_task_generation')
      if (phase==='cdc_alert_open') {
        stage='cdc_alert_open_rows'
        const rows=await compareRows('open')
        checks.push('opened_rows_match_api')
        const posts=new Map()
        page.on('request',req => {
          const match=new URL(req.url()).pathname.match(/^\/api\/v1\/monitor\/alerts\/(\d+)\/acknowledge$/)
          if (match && req.method()==='POST') posts.set(Number(match[1]),(posts.get(Number(match[1]))||0)+1)
        })
        stage='cdc_alert_acknowledge'
        for (const row of rows) {
          const visible=await findAlertRow(row.id)
          const response=page.waitForResponse(response => new URL(response.url()).pathname===`/api/v1/monitor/alerts/${row.id}/acknowledge` && response.request().method()==='POST')
          await visible.getByRole('button',{name:'确认',exact:true}).click()
          expect((await response).ok()).toBe(true)
        }
        await compareRows('acknowledged')
        expect([...posts.entries()].sort((a,b)=>a[0]-b[0])).toEqual(incidentIDs.map(id=>[id,1]).sort((a,b)=>a[0]-b[0]))
        checks.push('ui_acknowledge_confirmed')
      } else {
        stage='cdc_alert_retained_or_resolved'
        await compareRows(phase==='cdc_alert_unknown'?'acknowledged':'resolved')
        checks.push(phase==='cdc_alert_unknown'?'unknown_rows_match_api':'resolved_rows_match_api',
                    phase==='cdc_alert_unknown'?'last_observed_preserved':'original_incident_ids')
      }
      stage='cdc_alert_notifications'
      const destinations=await json(await alertAPI.get('/api/v1/monitor/webhook-destinations'),'CDC destination API')
      const destination=destinations.find(row=>row.id===binding.destination_id)
      expect(destination).toBeTruthy()
      const deliveries=await json(await alertAPI.get(`/api/v1/monitor/webhook-deliveries?destination_id=${binding.destination_id}&page_size=100`),'CDC delivery API')
      expect(deliveries.total).toBeLessThanOrEqual(100)
      const selected=deliveries.data.filter(row=>incidentIDs.includes(row.incident_id))
      expect(selected.map(row=>row.delivery_id).sort()).toEqual([...binding.delivery_ids].sort())
      expect(selected.filter(row=>row.event_type==='resolved')).toHaveLength(phase==='cdc_alert_resolved'?3:0)
      stage='cdc_alert_notification_page'
      await page.goto('/monitor/notifications')
      const card=frame.locator('.webhook-list .delivery-card')
      await expect(card).toBeVisible()
      await card.locator('.el-select').first().click()
      await frame.getByRole('option',{name:destination.name,exact:true}).click()
      stage='cdc_alert_notification_filter'
      const response=page.waitForResponse(response=>response.url().includes('/api/v1/monitor/webhook-deliveries?') && new URL(response.url()).searchParams.get('destination_id')===String(binding.destination_id))
      await card.getByRole('button',{name:'查询',exact:true}).click()
      expect((await response).ok()).toBe(true)
      // This owned destination may also receive other task signals; pin only these signed delivery IDs.
      async function findDeliveryRow(deliveryID) {
        const refreshed=page.waitForResponse(response=>response.url().includes('/api/v1/monitor/webhook-deliveries?') && new URL(response.url()).searchParams.get('destination_id')===String(binding.destination_id))
        await card.getByRole('button',{name:'查询',exact:true}).click()
        expect((await refreshed).ok()).toBe(true)
        await expect(card.locator('.el-loading-mask')).toHaveCount(0)
        for(let index=0;index<5;index++) {
          const row=card.locator('tbody tr').filter({hasText:deliveryID})
          if(await row.count()) return row
          const next=card.locator('.el-pagination .btn-next')
          if(await next.isDisabled()) break
          const response=page.waitForResponse(response=>response.url().includes('/api/v1/monitor/webhook-deliveries?'))
          await next.click();expect((await response).ok()).toBe(true)
          await expect(card.locator('.el-loading-mask')).toHaveCount(0)
        }
        throw new Error('bound signed delivery is absent from visible pages')
      }
      for (const row of selected) {
        stage='cdc_alert_delivery_'+row.event_type
        expect(row.status).toBe('delivered');expect(row.last_http_status).toBe(202)
        const visible=await findDeliveryRow(row.delivery_id)
        await expect(visible).toHaveCount(1)
        await expect(visible).toContainText(row.event_type==='opened'?'告警打开':'告警恢复')
        await expect(visible).toContainText('已送达')
        await expect(visible).toContainText('202')
        await expect(visible).toContainText(destination.name)
      }
      checks.push(phase==='cdc_alert_open'?'opened_delivery_rows_match_api':phase==='cdc_alert_unknown'?'no_recovery_delivery':'resolved_delivery_rows_match_api')
      await expect(alertPage).toHaveCount(0)
      await expect(card).not.toContainText('business-value-sentinel')
      checks.push('no_samples')
    } else if (history) {
      stage = 'cdc_history_chart'
      await expect(frame.getByRole('tab', { name: '运行资源', exact: true })).toHaveCount(0)
      checks.push(...await assertCDCTaskHistory(page, frame, history))
      await frame.getByTestId('cdc-task-history-chart').screenshot({ path: resolve(artifacts, 'continuous-cdc-history-chart.png') })
    } else if (phase.startsWith('cdc_')) {
      const binding = JSON.parse(process.env.ADDP_CONTINUOUS_BROWSER_CDC)
      const { task_id: taskID, engine_id: engineID } = binding
      stage = 'cdc_task_login'
      const { page, frame } = await signedPage(ids[0], true, `/monitor/cdc-resources?view=tasks&task_id=${taskID}`)
      await expect(frame.getByTestId('cdc-task-chain')).toBeVisible()
      await expect(frame.getByRole('tab', { name: '运行资源', exact: true })).toHaveCount(0)
      stage = 'cdc_task_metrics'
      checks.push(...await assertCDCTaskMonitoring(page, frame, binding, phase, ids[0]))
    } else if (phase === 'live') {
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
    if (alertAPI) await alertAPI.dispose()
    for (const context of contexts) await context.close()
  }
})
