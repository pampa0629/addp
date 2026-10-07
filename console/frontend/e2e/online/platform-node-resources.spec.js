import { expect, test } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { readFileSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { resourceMetrics } from '../../../../monitor/frontend/src/utils/nodeResources.js'
import { json, matchesRedirectURL } from './transfer-browser-support.js'

// Login failures must never capture MFA input or credentials.
test.use({ screenshot: 'off', trace: 'off' })

test('platform node resources through real Console password MFA and Monitor iframe', async ({ browser }) => {
  const artifact = process.env.ADDP_ONLINE_ARTIFACT_DIR
  const expected = JSON.parse(readFileSync(resolve(artifact, 'node-resources-browser-input.json'), 'utf8'))
  const repository = resolve(process.cwd(), '../..')
  const report = { schema_version: 'addp.node-resources-browser/v1', result: 'failed', run_id: expected.run_id }
  const save = stage => {
    report.stage = stage
    writeFileSync(resolve(artifact, 'node-resources-browser.json'), JSON.stringify(report, null, 2))
  }
  const context = await browser.newContext({ baseURL: process.env.CONSOLE_URL, locale: 'zh-CN', viewport: { width: 1440, height: 900 } })
  const page = await context.newPage()
  const resources = []
  const businessErrors = []
  const pending = []
  page.on('response', response => {
    const url = new URL(response.url())
    if (!/^\/api\/v1\/(system\/platform\/host_nodes|monitor\/platform\/resource_)/.test(url.pathname)) return
    if (!response.ok()) businessErrors.push(response.status())
    if (url.pathname.endsWith('resource_observations') || url.pathname.endsWith('resource_trends')) {
      pending.push(response.json().then(value => resources.push({ path: url.pathname, query: Object.fromEntries(url.searchParams), value })))
    }
  })
  const login = async (target, prefix, principalID, role) => {
    const identityResponse = target.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/system/auth/context' && response.ok(), { timeout: 90_000 })
    identityResponse.catch(() => {})
    await target.goto('/login?redirect=' + encodeURIComponent('/monitor/node-resources'))
    await target.locator('input[autocomplete="username"]').fill(process.env[prefix + '_USERNAME'])
    await target.locator('input[autocomplete="current-password"]').fill(process.env[prefix + '_PASSWORD'])
    const passwordResponse = target.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/system/login')
    await target.locator('.auth-login-primary').click()
    expect((await json(await passwordResponse, 'password login')).next_action).toBe('verify_mfa')
    await expect(target.locator('input[autocomplete="one-time-code"]')).toBeVisible()
    // Use a fresh TOTP time step, never replay the API phase's code or alter the clock.
    await new Promise(done => setTimeout(done, 30_000 - Date.now() % 30_000 + 150))
    const code = execFileSync('python3', ['-c', 'import importlib,os,time; print(importlib.import_module("scripts.test.platform-node-metrics-online").totp(os.environ["ADDP_BROWSER_TOTP"], time.time()))'], {
      cwd: repository, env: { ...process.env, ADDP_BROWSER_TOTP: process.env[prefix + '_TOTP'] }, encoding: 'utf8'
    }).trim()
    const mfaResponse = target.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/system/auth/mfa-verifications')
    await target.locator('input[autocomplete="one-time-code"]').fill(code)
    await target.locator('.auth-login-primary').click()
    expect((await json(await mfaResponse, 'MFA verification')).next_action).toBe('session_issued')
    const identity = await json(await identityResponse, 'browser AuthContext')
    expect(identity.principal).toMatchObject({ id: principalID, type: 'user' })
    expect(identity.context).toEqual({ type: 'platform' })
    expect(identity.delegation ?? null).toBeNull()
    expect(identity.authentication.assurance_level).toMatch(/^aal[23]$/)
    expect(identity.authorization.role_assignments.map(item => item.role_key)).toEqual([role])
    await expect(target).toHaveURL(/\/monitor\/node-resources$/)
    return { principal: { id: identity.principal.id, type: identity.principal.type }, context: identity.context, delegation: null, authentication: { assurance_level: identity.authentication.assurance_level }, token: { type: identity.token.type }, authorization: { role_assignments: identity.authorization.role_assignments.map(item => ({ role_key: item.role_key, permissions: item.permissions })) } }
  }
  try {
    save('administrator-password-mfa')
    report.identity = await login(page, 'ADDP_ONLINE_METRICS_ADMIN', expected.admin_id, 'platform.system_administrator')
    const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(monitor.getByTestId('resource-search')).toBeVisible()
    await expect(monitor.getByRole('button', { name: '查看资源' })).toHaveCount(1)
    await Promise.all(pending)
    expect(resources).toHaveLength(0)
    await page.screenshot({ path: resolve(artifact, 'node-resources-list.png'), animations: 'disabled' })
    const frame = page.frames().find(item => item.parentFrame())
    const documentID = await frame.evaluate(() => performance.timeOrigin)
    save('node-detail-eight-metrics')
    await monitor.getByRole('button', { name: '查看资源' }).click()
    await expect(page).toHaveURL(new RegExp(`/monitor/node-resources/${expected.node.node_id}$`))
    await expect(monitor.getByRole('heading', { name: expected.display_name, exact: true })).toBeVisible()
    await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
    expect(await frame.evaluate(() => performance.timeOrigin)).toBe(documentID)
    await Promise.all(pending)
    const instant = resources.find(item => item.path.endsWith('resource_observations'))
    expect(instant.query.node_id).toBe(expected.node.node_id)
    expect(instant.query.metrics.split(',').sort()).toEqual(resourceMetrics.map(item => item.key).sort())
    for (const metric of resourceMetrics) {
      const card = monitor.getByTestId(`resource-${metric.name}`)
      await expect(card).toBeVisible()
      await expect(card).toContainText('有效')
      await expect(card.locator('strong')).not.toHaveText('—')
    }
    await page.screenshot({ path: resolve(artifact, 'node-resources-detail.png'), animations: 'disabled' })
    save('history-and-trend-restore')
    await page.goBack()
    await expect(monitor.getByRole('button', { name: '查看资源' })).toHaveCount(1)
    await page.goForward()
    await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
    await monitor.getByTestId('resource-metric').click()
    await monitor.getByRole('option', { name: '1 分钟平均负载', exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`/monitor/node-resources/${expected.node.node_id}\\?metric=node.load.average_1m$`))
    await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
    await monitor.getByTestId('resource-range').click()
    await monitor.getByRole('option', { name: '近 5 分钟', exact: true }).click()
    await expect(page).toHaveURL(url => matchesRedirectURL(url, new URL(`/monitor/node-resources/${expected.node.node_id}?range=5m&metric=node.load.average_1m`, process.env.CONSOLE_URL)))
    await page.reload()
    await expect(monitor.getByTestId('resource-range')).toContainText('近 5 分钟')
    await expect(monitor.getByTestId('resource-metric')).toContainText('1 分钟平均负载')
    await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
    await Promise.all(pending)
    expect(resources.some(item => item.path.endsWith('resource_trends') && item.query.metrics === 'node.load.average_1m')).toBe(true)
    for (let index = 0; index < resources.length; index++) {
      const item = resources[index]
      if (!item.path.endsWith('resource_trends')) continue
      const anchor = resources.find(row => row.path.endsWith('resource_observations') && Date.parse(row.value.end) === Date.parse(item.query.end))
      expect(anchor).toBeTruthy()
      expect([300_000, 3_600_000]).toContain(Date.parse(item.query.end) - Date.parse(item.query.start))
    }
    await page.screenshot({ path: resolve(artifact, 'node-resources-restored.png'), animations: 'disabled' })
    expect(businessErrors).toEqual([])
    report.resources = resources
    report.navigation = { list_without_fanout: true, iframe_preserved: true, history: true, metric_reload: true, range_reload: true, server_window: true }
    save('security-administrator-denied')
    const negativeContext = await browser.newContext({ baseURL: process.env.CONSOLE_URL, locale: 'zh-CN' })
    try {
      const negative = await negativeContext.newPage()
      let nodeReads = 0
      negative.on('request', request => { if (/\/api\/v1\/(system\/platform\/host_nodes|monitor\/platform\/resource_)/.test(new URL(request.url()).pathname)) nodeReads++ })
      report.negative_identity = await login(negative, 'ADDP_ONLINE_METRICS_SECURITY', expected.security_id, 'platform.security_administrator')
      await expect(negative.locator('.el-result')).toBeVisible()
      await expect(negative.locator('iframe[data-testid="module-iframe"]')).toHaveCount(0)
      await expect(negative.locator('.sidebar .el-menu-item').filter({ hasText: '平台节点资源' })).toHaveCount(0)
      expect(nodeReads).toBe(0)
      report.negative_no_business_reads = true
    } finally { await negativeContext.close() }
    report.result = 'passed'
    save('complete')
  } catch (error) {
    // A bounded diagnostic is useful without exporting Playwright's login snapshots.
    let message = String(error.message)
    for (const [key, value] of Object.entries(process.env)) {
      if (/(PASSWORD|TOKEN|SECRET|TOTP)/.test(key) && value) message = message.split(value).join('[redacted]')
    }
    report.failure = message.replace(/addp_[a-z]+_[A-Za-z0-9_-]+/g, '[redacted]').replace(/\b\d{6}\b/g, '[redacted]').slice(0, 4000)
    save(report.stage)
    throw new Error('Node resource browser failed; see sanitized stage report')
  } finally { await context.close() }
})
