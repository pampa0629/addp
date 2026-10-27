import { expect, test } from '@playwright/test'
import { lstatSync, readFileSync, writeFileSync } from 'node:fs'
import { isAbsolute } from 'node:path'
import { localConsoleLogin } from './local-identity-support.mjs'

test.use({ screenshot: 'off', trace: 'off', video: 'off', locale: 'zh-CN' })

test('formal Python products to real Console HTTP quality', async ({ page }) => {
  test.setTimeout(240_000)
  const report = { schema: 'addp.http-quality-browser-receipt/v1', result: 'failed', stage: 'admission', assertions: {}, api_requests: 0 }
  let reportPath
  try {
    if (process.env.ADDP_HTTP_LOCAL !== '1' || ['GITHUB_ACTIONS', 'ADDP_ONLINE_HOSTED', 'ADDP_ONLINE_OWNER_MANAGED', 'ADDP_EXECUTION_TRACE_LOCAL'].some(key => process.env[key] !== undefined) || process.env.CONSOLE_URL !== 'http://nginx') throw new Error('http_browser_profile_denied')
    const config = process.env.ADDP_HTTP_LOCAL_BROWSER_CONFIG_FILE
    reportPath = process.env.ADDP_HTTP_LOCAL_BROWSER_REPORT_FILE
    if (!isAbsolute(config || '') || !isAbsolute(reportPath || '') || config === reportPath) throw new Error('http_browser_paths_denied')
    const file = lstatSync(config)
    if (!file.isFile() || file.isSymbolicLink() || file.nlink !== 1 || file.size > 65536 || file.mode & 0o077) throw new Error('http_browser_private_input_required')
    const input = JSON.parse(readFileSync(config, 'utf8'))
    if (input.schema !== 'addp.http-quality-browser/v1' || !/^addp-http-local-[a-f0-9]{32}$/.test(input.run_id) || input.instances?.length !== 2 || new Set(input.instances.map(row => row.module_name)).size !== 2 || input.instances.some(row => !['agent', 'copilot'].includes(row.module_name) || !Number.isSafeInteger(row.id) || row.id < 1 || row.role !== 'backend' || !/^[A-Za-z0-9_-]{1,100}$/.test(row.instance_id))) throw new Error('http_browser_input_invalid')
    report.run_id = input.run_id
    let requestFailure
    page.on('request', request => {
      const url = new URL(request.url())
      if (!url.pathname.startsWith('/api/v1/')) return
      report.api_requests++
      if (report.api_requests > 80 || url.origin !== 'http://nginx' || !['GET', 'POST'].includes(request.method()) || (request.method() === 'POST' && !['/api/v1/system/login', '/api/v1/system/auth/mfa-verifications', '/api/v1/system/logout'].includes(url.pathname)) || /\/api\/v1\/(agent|copilot)\//.test(url.pathname)) requestFailure = 'http_browser_request_scope_exceeded'
    })
    report.stage = 'password-mfa'
    await localConsoleLogin(page, expect, { prefix: 'ADDP_HTTP_LOCAL_', path: '/monitor/http-quality?module_name=agent&refresh=off' })
    report.assertions.real_password_mfa = true
    const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
    report.stage = 'http-quality-pages'
    for (const owner of input.instances) {
      const observations = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/monitor/platform/http_observations' && response.ok())
      observations.catch(() => {})
      await page.goto('/monitor/http-quality?module_name=' + owner.module_name + '&refresh=off')
      const summary = await (await observations).json()
      expect(summary.data.some(row => row.subject.id === owner.id && row.subject.instance_id === owner.instance_id && row.collection.state === 'collecting')).toBe(true)
      await expect(monitor.getByTestId('http-quality')).toBeVisible()
      await expect(monitor.getByTestId('http-error')).toHaveCount(0)
      const row = monitor.getByTestId('http-instances').getByRole('row').filter({ hasText: owner.instance_id })
      await expect(row).toContainText(owner.module_name)
      const detail = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/monitor/platform/http_interfaces' && response.ok())
      const trend = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/monitor/platform/http_trends' && response.ok())
      detail.catch(() => {}); trend.catch(() => {})
      await row.getByRole('button', { name: '接口与趋势', exact: true }).click()
      expect((await (await detail).json()).data[0].subject.id).toBe(owner.id)
      expect((await (await trend).json()).data[0].subject.id).toBe(owner.id)
      await expect(monitor.getByTestId('http-detail-error')).toHaveCount(0)
      await expect(monitor.getByTestId('http-interfaces')).toBeVisible()
      await expect(monitor.getByTestId('http-trend')).toBeVisible()
    }
    await page.getByTitle('中文', { exact: true }).click()
    await page.getByRole('menuitem', { name: 'English', exact: true }).click()
    await expect(monitor.getByTestId('http-quality')).toContainText('HTTP quality')
    await expect(monitor.getByTestId('http-quality')).toContainText('Interfaces and trends')
    report.assertions.chinese_and_english = true
    if (requestFailure) throw new Error(requestFailure)
    report.assertions.real_summary_ranking_trend_two_sources = true
    report.result = 'passed'; report.stage = 'complete'
  } catch (error) {
    report.error_code = /^[a-z0-9_]+$/.test(error.message || '') ? error.message : 'formal_http_browser_assertion_failed'
    throw new Error(report.error_code)
  } finally {
    if (reportPath && isAbsolute(reportPath)) writeFileSync(reportPath, JSON.stringify(report, null, 2), { mode: 0o600, flag: 'wx' })
  }
})
