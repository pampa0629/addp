import { expect, test } from '@playwright/test'
import { lstatSync, readFileSync, writeFileSync } from 'node:fs'
import { isAbsolute } from 'node:path'
import { localConsoleLogin } from './local-identity-support.mjs'
import { httpResponses, assertHTTPPage, assertHTTPRestored, assertHTTPStoppedPage } from '../http-quality-page-support.mjs'

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
    if (!['live', 'stopped'].includes(input.phase) || !Number.isInteger(input.request_limit) || input.request_limit < 1 || input.request_limit > 80 || (input.phase === 'stopped' ? !Number.isFinite(Date.parse(input.stopped_at)) || Date.parse(input.stopped_at) > Date.now() : input.stopped_at !== null)) throw new Error('http_browser_phase_invalid')
    report.run_id = input.run_id; report.phase = input.phase
    let requestFailure
    // Reject out-of-scope transport before it reaches a backend. Permitted
    // requests continue unchanged; no response, identity or metrics are mocked.
    await page.route('**/api/v1/**', async route => {
      const request = route.request(), url = new URL(request.url())
      const allowed = !requestFailure && report.api_requests < input.request_limit && url.origin === 'http://nginx' &&
        ['GET', 'POST'].includes(request.method()) &&
        (request.method() !== 'POST' || ['/api/v1/system/login', '/api/v1/system/auth/mfa-verifications', '/api/v1/system/logout'].includes(url.pathname)) &&
        !/\/api\/v1\/(agent|copilot)\//.test(url.pathname)
      if (!allowed) {
        requestFailure = 'http_browser_request_scope_exceeded'
        await route.abort('blockedbyclient')
        await page.close()
        return
      }
      report.api_requests++
      await route.continue()
    })
    report.stage = 'password-mfa'
    await localConsoleLogin(page, expect, { prefix: 'ADDP_HTTP_LOCAL_', path: '/monitor/http-quality?module_name=agent&refresh=off' })
    report.assertions.real_password_mfa = true
    const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
    report.stage = 'http-quality-pages'
    await expect(monitor.getByTestId('http-instances')).toContainText(input.instances.find(row => row.module_name === 'agent').instance_id)
    for (const owner of input.instances) {
      const query = { module_name: owner.module_name, refresh: 'off', instance_id: String(owner.id), range: '5m', sort: 'qps' }
      const path = '/monitor/http-quality?' + new URLSearchParams(query)
      if (input.phase === 'stopped' && owner.module_name === 'copilot') {
        const listed = page.waitForResponse(response => {
          const url = new URL(response.url())
          return url.pathname === '/api/v1/system/platform/module-instances' && url.searchParams.get('module_name') === 'copilot' && url.searchParams.get('role') === 'backend' && url.searchParams.get('status') === 'up' && response.ok()
        })
        listed.catch(() => {})
        await page.goto(path)
        expect((await (await listed).json()).data).toEqual([])
        await assertHTTPStoppedPage(expect, monitor, owner)
        report.assertions.stopped_page_empty = true
        continue
      }
      const payloads = await httpResponses(page, () => page.goto(path), { owner })
      await assertHTTPPage(expect, monitor, owner, payloads, { sort: 'qps', stoppedAt: input.stopped_at })
      if (input.phase === 'live') {
        // Exercise actual template selection, then wait for both public routes
        // to commit before refreshing. Default window/backend are canonical omissions.
        const route = payloads[1].data[0].observations.find(row => row.route && row.method === 'GET')
        expect(route).toBeTruthy()
        const filtered = await httpResponses(page, () => monitor.getByTestId('http-interfaces').getByRole('button', { name: route.method + ' ' + route.route, exact: true }).click(), { owner })
        Object.assign(query, { route: route.route, method: route.method })
        await assertHTTPPage(expect, monitor, owner, filtered, { sort: 'qps' })
        await assertHTTPRestored(expect, page, monitor, query)
        if (owner.module_name === 'agent') {
          const restored = await httpResponses(page, () => page.reload(), { owner })
          await assertHTTPRestored(expect, page, monitor, query)
          await assertHTTPPage(expect, monitor, owner, restored, { sort: 'qps' })
          report.assertions.url_reload_restored = true
        }
      } else report.assertions.agent_sample_after_stop = true
    }
    report.assertions.summary_ranking_trend_api_values = true
    if (input.phase === 'live') {
      await page.getByTitle('中文', { exact: true }).click()
      await page.getByRole('menuitem', { name: 'English', exact: true }).click()
      await expect(monitor.getByTestId('http-quality')).toContainText('HTTP quality')
      await expect(monitor.getByTestId('http-quality')).toContainText('Interfaces and trends')
      report.assertions.chinese_and_english = true
    }
    // Freeze successful counts only after this page can issue no more reads.
    await page.close()
    if (requestFailure) throw new Error(requestFailure)
    report.result = 'passed'; report.stage = 'complete'
  } catch (error) {
    report.error_code = /^[a-z0-9_]+$/.test(error.message || '') ? error.message : 'formal_http_browser_assertion_failed'
    throw new Error(report.error_code)
  } finally {
    if (reportPath && isAbsolute(reportPath)) writeFileSync(reportPath, JSON.stringify(report, null, 2), { mode: 0o600, flag: 'wx' })
  }
})
