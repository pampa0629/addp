import { createHash } from 'node:crypto'
import { performance } from 'node:perf_hooks'
import { expect } from '@playwright/test'

const events = {
  workflow_started: '工作流开始执行',
  task_progress: '节点进度更新',
  workflow_succeeded: '工作流执行成功',
  workflow_failed: '工作流执行失败'
}

// Keep existing synthetic geometry and diagnostics, including on partial failure.
export function differenceArchive(value) {
  if (Array.isArray(value)) return value.map(differenceArchive)
  if (!value || typeof value !== 'object') return value
  const projected = Object.fromEntries(Object.entries(value).map(([key, entry]) => [key, differenceArchive(entry)]))
  if (projected.metadata?.result && typeof projected.metadata.result === 'object') {
    delete projected.metadata.result.logs
  }
  return projected
}

export async function verifyDifferenceLogPages(page, samples, report, persist) {
  if (samples.length !== 5 || new Set(samples.map(sample => sample.execution_id)).size !== 5) {
    throw new Error('execution_logs_sample_mismatch')
  }
  const started = performance.now()
  const deadline = started + 60_000
  let stage = 'language'
  let sampleIndex = 0
  let httpStatus = null
  const options = () => {
    const remaining = Math.floor(deadline - performance.now())
    if (remaining <= 0) throw new Error('execution_logs_deadline')
    return { timeout: Math.min(5000, remaining) }
  }
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  const reads = []
  const observe = request => {
    if (request.method() === 'GET' && /^\/api\/v1\/develop\/executions\/[^/]+\/logs$/.test(new URL(request.url()).pathname)) {
      reads.push(new URL(request.url()).pathname)
      report.reads = reads.length
    }
  }
  async function readLogs(previousReads, sample, action) {
    const path = '/api/v1/develop/executions/' + sample.execution_id + '/logs'
    const [response] = await Promise.all([
      page.waitForResponse(async response => {
        if (response.request().method() !== 'GET' || new URL(response.url()).pathname !== path) return false
        httpStatus = response.status()
        // Wait for the complete response within the same request deadline.
        await response.finished()
        return true
      }, options()),
      action()
    ])
    httpStatus = response.status()
    if (httpStatus !== 200) throw new Error('execution_logs_http_failed')
    const body = await response.json()
    options()
    if (body?.execution_id !== sample.execution_id || !Array.isArray(body.logs) || body.logs.length !== sample.event_count) {
      throw new Error('execution_logs_payload_mismatch')
    }
    const wire = JSON.stringify(body.logs.map(log => Object.fromEntries(Object.keys(log).sort().map(key => [key, log[key]]))))
    if (createHash('sha256').update(wire).digest('hex') !== sample.sha256) {
      throw new Error('execution_logs_digest_mismatch')
    }
    if (reads.length !== previousReads + 1 || reads.at(-1) !== path) throw new Error('execution_logs_read_count')
    return body.logs
  }
  async function verifyTimeline(logs) {
    const timeline = frame.locator('#pane-logs .el-timeline-item')
    // Compare privately so Playwright's failure output cannot include a response or full timeline.
    await expect.poll(async () => {
      const entries = await timeline.evaluateAll(items => items.map(item => ({
        timestamp: item.querySelector('.el-timeline-item__timestamp')?.textContent.trim(),
        level: item.querySelector('.log-item .el-tag')?.textContent.trim(),
        message: item.querySelector('.log-message')?.textContent.trim(),
        progress: item.querySelector('.log-message + span')?.textContent.trim()
      })))
      return entries.length === logs.length && entries.every((entry, index) => {
        const log = logs[index]
        return Boolean(entry.timestamp) && entry.timestamp !== '-' && entry.level === log.level &&
          entry.message === events[log.event] && entry.progress === (log.event === 'task_progress'
            ? '节点 ' + log.task_id + ' · ' + log.progress + '%' : undefined)
      })
    }, { ...options(), message: 'execution_logs_timeline_mismatch' }).toBe(true)
  }
  page.on('request', observe)
  try {
    await page.locator('.header-language button').click(options())
    await page.getByRole('menuitem', { name: '中文', exact: true }).click(options())
    for (const [index, sample] of samples.entries()) {
      sampleIndex = index
      httpStatus = null
      stage = 'monitor'
      const id = sample.execution_id
      const monitorURL = '/monitor/executions?execution_id=' + id
      const ownerPath = '/develop/executions/' + id
      const ownerURL = url => url.pathname === ownerPath && url.searchParams.get('return_to') === monitorURL
      const logURL = url => ownerURL(url) && url.searchParams.get('tab') === 'logs'
      await page.goto(monitorURL, options())
      await expect(frame.getByRole('dialog')).toBeVisible(options())
      stage = 'enter'
      const logs = await readLogs(index * 2, sample, () =>
        frame.getByRole('button', { name: '查看执行结果', exact: true }).click(options()))
      await expect(page).toHaveURL(ownerURL, options())
      stage = 'timeline'
      await frame.getByRole('tab', { name: '执行日志', exact: true }).click(options())
      await expect(page).toHaveURL(logURL, options())
      await verifyTimeline(logs)
      stage = 'reload'
      httpStatus = null
      const reloaded = await readLogs(index * 2 + 1, sample, () => page.reload(options()))
      await expect(page).toHaveURL(logURL, options())
      await verifyTimeline(reloaded)
      stage = 'return'
      await frame.getByRole('button', { name: '返回本次执行', exact: true }).click(options())
      await expect(page).toHaveURL(monitorURL, options())
      await expect(frame.getByRole('dialog')).toBeVisible(options())
      report.samples.push({ ...sample, timeline: true, reload: true, return_to_monitor: true })
      report.elapsed_ms = Math.round(performance.now() - started)
      persist('execution_logs_observed')
    }
    if (reads.length !== 10) throw new Error('execution_logs_read_count')
    report.elapsed_ms = Math.round(performance.now() - started)
    persist('execution_logs_verified')
  } catch (error) {
    const code = ['execution_logs_deadline', 'execution_logs_http_failed', 'execution_logs_payload_mismatch',
      'execution_logs_digest_mismatch', 'execution_logs_read_count'].includes(error.message) ? error.message : 'execution_logs_browser_failed'
    report.first_failure = { stage, sample: sampleIndex, http_status: httpStatus, code }
    report.elapsed_ms = Math.round(performance.now() - started)
    try { persist('execution_logs_failed') } catch { /* Preserve the first failure. */ }
    throw new Error(code + ' at ' + stage)
  } finally {
    page.off('request', observe)
  }
}
