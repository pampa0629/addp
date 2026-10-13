import { expect, request, test } from '@playwright/test'
import { existsSync, readFileSync, renameSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { identity, json, login } from './transfer-browser-support.js'

// Only explicitly verified screenshots are public; failure artifacts stay private.
test.use({ screenshot: 'off' })

test('continuous execution timeline, recovery and revoked Owner access', async ({ browser }) => {
  test.setTimeout(180_000)
  if (process.env.ADDP_ONLINE_HOSTED !== '1' || process.env.ADDP_ONLINE_TEST !== '1') throw new Error('dedicated Hosted entry required')
  const phase = process.env.ADDP_CONTINUOUS_BROWSER_PHASE
  const ids = JSON.parse(process.env.ADDP_CONTINUOUS_BROWSER_EXECUTIONS)
  const runID = process.env.ADDP_ONLINE_TEST_RUN_ID
  const tenantID = process.env.ADDP_ONLINE_TEST_TENANT_ID
  const principalID = process.env.ADDP_CONTINUOUS_BROWSER_PRINCIPAL
  const control = process.env.ADDP_CONTINUOUS_BROWSER_CONTROL
  const artifacts = process.env.ADDP_ONLINE_ARTIFACT_DIR
  const credentials = [process.env.ADDP_CONTINUOUS_BROWSER_USERNAME, process.env.ADDP_CONTINUOUS_BROWSER_PASSWORD]
  if (!['live', 'recovery', 'revocation'].includes(phase) || !control || !artifacts || credentials.some(value => !value)) throw new Error('incomplete browser scope')
  const marker = { run_id: runID, phase, execution_id: ids[0] }
  const checks = []
  let stage = 'login'
  const contexts = []
  const report = result => ({ schema_version: 'addp.continuous-execution-browser/v1', phase,
    run_id: runID, tenant_id: tenantID, principal_id: principalID, execution_ids: ids, checks, result })
  const destination = resolve(artifacts, `continuous-browser-${phase}.json`)
  const route = id => `/monitor/executions?execution_id=${encodeURIComponent(id)}`
  async function signedPage(id, owner = true) {
    const context = await browser.newContext({ baseURL: process.env.CONSOLE_URL, locale: 'zh-CN' })
    contexts.push(context)
    const page = await context.newPage()
    await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
    const token = await login(page, ...credentials, route(id))
    const api = await request.newContext({ baseURL: process.env.GATEWAY_URL, extraHTTPHeaders: { Authorization: `Bearer ${token}` } })
    try {
      const current = identity(await json(await api.get('/api/v1/system/auth/context'), 'browser AuthContext'))
      expect(current.principalType).toBe('user')
      expect(current.contextType).toBe('tenant')
      expect(current.principalID).toBe(principalID)
      expect(current.tenantID).toBe(tenantID)
      expect(current.permissions.has('monitor.execution.read')).toBe(true)
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
  async function release() {
    const temporary = resolve(control, 'ready.tmp')
    writeFileSync(temporary, JSON.stringify(marker))
    renameSync(temporary, resolve(control, 'ready.json'))
    await expect.poll(() => {
      const file = resolve(control, 'release.json')
      return existsSync(file) ? JSON.parse(readFileSync(file, 'utf8')) : null
    }, { timeout: 100_000, intervals: [200] }).toEqual(marker)
  }
  try {
    const { page, frame } = await signedPage(ids[0])
    if (phase === 'live') {
      stage = 'live_timeline'
      const { events } = await timeline(frame)
      await expect(events).toContainText('写入记录: 2')
      checks.push('timeline')
      stage = 'live_automatic_progress'
      await release()
      // Python commits additional physical Kafka records while this page stays open.
      await expect(events).toContainText('写入记录: 4')
      checks.push('automatic_committed_progress')
      await expect(events.locator('tbody tr').filter({ hasText: '开始执行' })).toHaveCount(1)
      await expect(events).not.toContainText('business-value-sentinel')
      checks.push('no_samples')
      await page.screenshot({ path: resolve(artifacts, 'continuous-live-events.png'), fullPage: true })
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
      await expect(fresh.frame.getByText('未能加载目标执行详情', { exact: true })).toBeVisible()
      await expect(fresh.frame.locator('.execution-detail-content')).toHaveCount(0)
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
