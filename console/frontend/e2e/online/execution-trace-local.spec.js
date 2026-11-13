import { expect, test } from '@playwright/test'
import { lstatSync, readFileSync, writeFileSync } from 'node:fs'
import { isAbsolute } from 'node:path'
import { executionTraceBrowserInput, admitExecutionTraceBrowserEnvironment, tenantStageBrowserInput, tenantStagePermissions } from './execution-trace-local-support.mjs'

import { assertHTTPBrowserLogout } from '../http-quality-page-support.mjs'
import { localConsoleLogin } from './local-identity-support.mjs'

// A failed password or MFA action must never create browser captures.
test.use({ screenshot: 'off', trace: 'off', video: 'off', locale: 'zh-CN' })

test('execution trace local real Console password MFA, causal link and checkpoint logs', async ({ page }) => {
  test.setTimeout(240_000)
  const report = { schema: 'addp.execution-trace-browser-receipt/v1', result: 'failed', stage: 'admission', assertions: {} }
  let reportPath
  try {
    admitExecutionTraceBrowserEnvironment(process.env)
    const inputPath = process.env.ADDP_EXECUTION_TRACE_BROWSER_CONFIG_FILE
    reportPath = process.env.ADDP_EXECUTION_TRACE_BROWSER_REPORT_FILE
    if (!isAbsolute(inputPath || '') || !isAbsolute(reportPath || '') || reportPath === inputPath) throw new Error('browser_private_paths_invalid')
    const info = lstatSync(inputPath)
    if (!info.isFile() || info.isSymbolicLink() || info.nlink !== 1 || (info.mode & 0o077) !== 0 || info.size > 65536) throw new Error('browser_private_input_invalid')
    const input = executionTraceBrowserInput(JSON.parse(readFileSync(inputPath, 'utf8')))
    report.run_id = input.run_id
    report.stage = 'password-mfa'
    let initialTraceResponse
    const identity = await localConsoleLogin(page, expect, {
      prefix: 'ADDP_EXECUTION_TRACE_', path: input.trace_path,
      beforeMFA: () => {
        initialTraceResponse = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/monitor/platform/traces/' + input.trace_id)
        initialTraceResponse.catch(() => {})
      }
    })
    if (input.admin_id !== undefined) expect(identity.principal.id).toBe(input.admin_id)
    report.assertions.password_mfa_platform_user = true
    report.stage = 'retained-terminal'
    const initialResponse = await initialTraceResponse
    expect(initialResponse.status()).toBe(200)
    const initial = await initialResponse.json()
    const stage = initial.data?.[0]?.spans?.find(span => span.span_id === input.span_id)
    expect(Boolean(stage && stage.scope === 'addp.execution' && stage.phase === 'terminal' && stage.outcome === 'failed' && stage.failed && Number.isInteger(stage.attempt) && stage.attempt >= 1 && stage.module_name === input.module_name && stage.instance_id === input.instance_id)).toBe(true)
    expect(initial.data[0].trace_id).toBe(input.trace_id)
    const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(monitor.getByTestId('trace-spans')).toContainText('终态')
    await expect(monitor.getByTestId('trace-spans')).toContainText('失败')
    await expect(monitor.getByTestId('trace-spans')).toContainText(input.span_id)
    report.assertions.retained_failed_terminal_visible = true
    report.stage = 'causal-link'
    const target = input.links[0]
    expect(Boolean(stage.links?.some(link => Object.keys(target).every(key => link[key] === target[key])))).toBe(true)
    const instanceResponse = page.waitForResponse(response => {
      const url = new URL(response.url())
      return url.pathname === '/api/v1/system/platform/module-instances' && url.searchParams.get('ids') === String(target.runtime_instance_id)
    })
    const targetResponse = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/monitor/platform/traces/' + target.trace_id)
    instanceResponse.catch(() => {}); targetResponse.catch(() => {})
    await monitor.getByTestId('trace-links').getByRole('row').filter({ hasText: target.trace_id }).first().getByRole('button', { name: '查看关联', exact: true }).click()
    const instanceRead = await instanceResponse
    expect(instanceRead.status()).toBe(200)
    const instances = await instanceRead.json()
    expect(Boolean(instances.data?.some(row => row.id === target.runtime_instance_id && row.instance_id === target.instance_id && row.module_name === target.module_name && row.role === target.role))).toBe(true)
    const linkedResponse = await targetResponse
    expect([200, 404].includes(linkedResponse.status())).toBe(true)
    const linkURL = new URL(linkedResponse.url())
    expect(linkURL.searchParams.get('instance_id')).toBe(String(target.runtime_instance_id))
    expect(Date.parse(linkURL.searchParams.get('start'))).toBe(Date.parse(target.started_at) - 1000)
    expect(Date.parse(linkURL.searchParams.get('end'))).toBe(Date.parse(target.started_at) + 1000)
    if (linkedResponse.status() === 404) {
      expect((await linkedResponse.json()).error_code).toBe('trace_not_found')
      await expect(monitor.getByTestId('trace-error')).toContainText('没有可读的已保留调用链')
      await expect(monitor.getByTestId('trace-spans')).toHaveCount(0)
    } else {
      expect((await linkedResponse.json()).data?.[0]?.trace_id).toBe(target.trace_id)
      await expect(monitor.getByTestId('trace-spans')).toBeVisible()
      await expect(monitor.getByTestId('trace-error')).toHaveCount(0)
    }
    report.assertions.link_instance_currently_readable = true
    report.assertions.link_target_time_window = true
    report.assertions.link_lookup_bounded_result = true
    report.link_result = linkedResponse.status() === 200 ? 'retained' : 'not_found'
    report.stage = 'checkpoint-log'
    await page.goto(input.trace_path)
    await expect(monitor.getByTestId('trace-spans')).toContainText(input.span_id)
    const logPath = '/api/v1/system/platform/modules/' + encodeURIComponent(input.module_name) + '/instances/' + encodeURIComponent(input.instance_id) + '/logs'
    const logsResponse = page.waitForResponse(response => {
      const url = new URL(response.url())
      return url.pathname === logPath && url.searchParams.get('trace_id') === input.trace_id
    })
    logsResponse.catch(() => {})
    await monitor.getByTestId('trace-spans').getByRole('row').filter({ hasText: input.span_id }).getByRole('button', { name: '关联日志', exact: true }).click()
    const logsRead = await logsResponse
    expect(logsRead.status()).toBe(200)
    const logs = await logsRead.json()
    expect(Boolean(logs.entries?.some(entry => {
      if (entry.trace_id !== input.trace_id || entry.span_id !== input.span_id) return false
      let body; try { body = JSON.parse(entry.message) } catch { return false }
      return body.msg === 'execution.checkpoint' && body.execution_id === input.execution_id && body.phase === 'terminal' && body.outcome === 'failed' && body.attempt === stage.attempt
    }))).toBe(true)
    const system = page.frameLocator('iframe[data-testid="module-iframe"]')
    const checkpoint = system.getByTestId('runtime-log-output').locator('article').filter({ hasText: input.span_id })
    await expect(checkpoint).toContainText('execution.checkpoint')
    await expect(checkpoint).toContainText(input.execution_id)
    await expect(checkpoint).toContainText(input.trace_id)
    report.assertions.exact_checkpoint_trace_span_visible = true
    report.stage = 'platform-browser-logout'
    expect(await assertHTTPBrowserLogout(page, expect, 2)).toBe(2)
    report.assertions.platform_browser_logged_out = true

    report.stage = 'tenant-input'
    const tenantPath = process.env.ADDP_EXECUTION_TRACE_TENANT_BROWSER_CONFIG_FILE
    if (!isAbsolute(tenantPath || '') || tenantPath === inputPath || tenantPath === reportPath) throw new Error('tenant_private_path_invalid')
    const tenantFile = lstatSync(tenantPath)
    if (!tenantFile.isFile() || tenantFile.isSymbolicLink() || tenantFile.nlink !== 1 || (tenantFile.mode & 0o077) !== 0 || tenantFile.size > 65536) throw new Error('tenant_private_input_invalid')
    const tenant = tenantStageBrowserInput(JSON.parse(readFileSync(tenantPath, 'utf8')))
    expect(tenant.run_id).toBe(input.run_id)
    const tenantContext = await page.context().browser().newContext({ baseURL: process.env.CONSOLE_URL, locale: 'zh-CN' })
    try {
      const tenantPage = await tenantContext.newPage()
      let stageRequests = 0
      let platformRequests = 0
      tenantPage.on('request', request => {
        const path = new URL(request.url()).pathname
        if (path.endsWith('/trace-stages')) stageRequests++
        if (path.includes('/platform/traces') || path.includes('/platform/modules/') && path.endsWith('/logs')) platformRequests++
      })
      report.stage = 'tenant-password'
      const firstPath = '/monitor/executions?execution_id=' + tenant.cases[0].execution_id
      const user = await localConsoleLogin(tenantPage, expect, { prefix: 'ADDP_EXECUTION_TRACE_', principal: 'USER', scope: 'tenant', path: firstPath })
      expect(user.principal.id).toBe(tenant.user_id)
      expect(user.context.tenant_id).toBe(tenant.tenant_id)
      expect([...new Set(user.authorization.role_assignments.flatMap(row => row.permissions))].sort()).toEqual([...tenantStagePermissions].sort())
      report.assertions.tenant_real_login_exact_permissions = true
      const frame = tenantPage.frameLocator('iframe[data-testid="module-iframe"]')
      for (const [index, sample] of tenant.cases.entries()) {
        report.stage = 'tenant-' + sample.module + '-' + sample.kind
        if (index) await tenantPage.goto('/monitor/executions?execution_id=' + sample.execution_id)
        const panel = frame.locator('.execution-trace-stages')
        await expect(panel).toBeVisible()
        expect(stageRequests).toBe(index)
        const responsePromise = tenantPage.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/monitor/executions/by-execution-id/' + sample.execution_id + '/trace-stages')
        await panel.getByRole('button', { name: '查看阶段追踪', exact: true }).click()
        const response = await responsePromise
        expect(response.status()).toBe(200)
        expect(response.headers()['cache-control']).toBe('no-store')
        const actual = await response.json()
        // Fail on new/raw fields as well as changed samples. Tenant input has no technical IDs.
        tenantStageBrowserInput({ ...tenant, cases: tenant.cases.map(row => row === sample ? { ...row, stages: actual.stages } : row) })
        expect(actual.execution_id).toBe(sample.execution_id)
        expect(actual.supported).toBe(true)
        expect(Object.keys(actual).sort()).toEqual(['execution_id', 'stages', 'supported'])
        if (sample.kind === 'readable') {
          expect(actual.stages[1]).toEqual(sample.stages[1])
          await expect(panel.locator('.stage-card').last()).toContainText('失败')
        } else {
          expect(actual.stages[0]).toEqual({ slot: 'enqueue', state: 'not_found' })
          await expect(panel.locator('.stage-card').first()).toContainText('不能据此判断执行结果')
        }
        await expect(frame.locator('.execution-detail-content')).toBeVisible()
        await panel.getByRole('button', { name: '收起阶段追踪', exact: true }).click()
        await expect(panel.locator('.stage-card')).toHaveCount(0)
        await frame.locator('.execution-detail-dialog .el-dialog__headerbtn').click()
        await expect(frame.locator('.execution-trace-stages')).toHaveCount(0)
      }
      expect(stageRequests).toBe(4)
      expect(platformRequests).toBe(0)
      report.stage = 'tenant-browser-logout'
      expect(await assertHTTPBrowserLogout(tenantPage, expect, 2)).toBe(2)
      report.assertions.tenant_browser_lazy_readable_missing_clear_logout = true
    } finally { await tenantContext.close() }
    report.result = 'passed'; report.stage = 'complete'
  } catch {
    // Do not export Playwright's password-fill call logs, response bodies or URL
    // snapshots. The caller keeps only this fixed stage and safe booleans.
    throw new Error('Execution Trace local browser failed at stage ' + report.stage)
  } finally {
    if (reportPath) writeFileSync(reportPath, JSON.stringify(report, null, 2), { mode: 0o600, flag: 'wx' })
  }
})
