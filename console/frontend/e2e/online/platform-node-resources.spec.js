import { expect, test } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { readFileSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { resourceMetrics, filesystemMetrics, inodeMetrics, diskMetrics, networkMetrics, resourceDimensionRows } from '../../../../monitor/frontend/src/utils/nodeResources.js'
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
    save('node-detail-catalog-metrics')
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
    for (const name of ['memoryTotal', 'memoryAvailable']) {
      await expect(monitor.getByTestId(`resource-${name}`).locator('strong')).toHaveText(/\d[\d,.]* (?:B|KiB|MiB|GiB|TiB|PiB)$/)
    }
    await expect(monitor.getByTestId('resource-uptime').locator('strong')).toHaveText(/\d+(?:天|小时|分钟|秒)/)
    for (const name of ['load1m', 'load5m', 'load15m']) {
      const card = monitor.getByTestId(`resource-${name}`)
      await expect(card.locator('strong')).toHaveText(/^约 [\d,]+\.\d{2} 个任务$/)
      await expect(card.getByText('平均正在运行或等待的任务数，非百分比。', { exact: true })).toBeVisible()
    }
    report.presentation = { iec_capacity: true, elapsed_uptime: true, system_load_count: true, disk_rate_units: false, network_rate_units: false }
    await page.screenshot({ path: resolve(artifact, 'node-resources-detail.png'), animations: 'disabled' })
    save('history-and-trend-restore')
    await page.goBack()
    await expect(monitor.getByRole('button', { name: '查看资源' })).toHaveCount(1)
    await page.goForward()
    await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
    await monitor.getByTestId('resource-metric').click()
    await monitor.getByRole('option', { name: 'CPU 忙碌率（1 分钟）', exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`/monitor/node-resources/${expected.node.node_id}\\?metric=node.cpu.busy_percent$`))
    await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
    await monitor.getByTestId('resource-range').click()
    await monitor.getByRole('option', { name: '近 5 分钟', exact: true }).click()
    await expect(page).toHaveURL(url => matchesRedirectURL(url, new URL(`/monitor/node-resources/${expected.node.node_id}?range=5m&metric=node.cpu.busy_percent`, process.env.CONSOLE_URL)))
    await page.reload()
    await expect(monitor.getByTestId('resource-range')).toContainText('近 5 分钟')
    await expect(monitor.getByTestId('resource-metric')).toContainText('CPU 忙碌率（1 分钟）')
    await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
    await Promise.all(pending)
    expect(resources.some(item => item.path.endsWith('resource_trends') && item.query.metrics === 'node.cpu.busy_percent')).toBe(true)
    for (let index = 0; index < resources.length; index++) {
      const item = resources[index]
      if (!item.path.endsWith('resource_trends')) continue
      const anchor = resources.find(row => row.path.endsWith('resource_observations') && Date.parse(row.value.end) === Date.parse(item.query.end))
      expect(anchor).toBeTruthy()
      expect([300_000, 3_600_000]).toContain(Date.parse(item.query.end) - Date.parse(item.query.start))
    }
    save('natural-auto-refresh-and-off-restore')
    const beforeAuto = resources.filter(item => item.path.endsWith('resource_observations')).length
    const priorEnd = resources.filter(item => item.path.endsWith('resource_observations')).at(-1).value.end
    const refreshURL = page.url()
    await expect(monitor.getByTestId('resource-refresh')).toContainText('每 15 秒')
    const autoInstant = page.waitForResponse(response => new URL(response.url()).pathname.endsWith('resource_observations') && response.ok())
    const autoTrend = page.waitForResponse(response => new URL(response.url()).pathname.endsWith('resource_trends') && response.ok())
    await autoInstant
    await autoTrend
    await Promise.all(pending)
    expect(resources.filter(item => item.path.endsWith('resource_observations')).length).toBeGreaterThan(beforeAuto)
    expect(Date.parse(resources.filter(item => item.path.endsWith('resource_observations')).at(-1).value.end)).toBeGreaterThan(Date.parse(priorEnd))
    expect(page.url()).toBe(refreshURL)
    await monitor.getByTestId('resource-refresh').click()
    await monitor.getByRole('option', { name: '关闭自动刷新', exact: true }).click()
    await expect(page).toHaveURL(url => matchesRedirectURL(url, new URL(`/monitor/node-resources/${expected.node.node_id}?range=5m&metric=node.cpu.busy_percent&refresh=off`, process.env.CONSOLE_URL)))
    await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
    await page.reload()
    await expect(monitor.getByTestId('resource-refresh')).toContainText('关闭自动刷新')
    await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
    await Promise.all(pending)
    const offCount = resources.filter(item => item.path.endsWith('resource_observations')).length
    // Natural wall clock: no injected timer or intercepted API in Hosted T4.
    await new Promise(done => setTimeout(done, 16_000))
    await Promise.all(pending)
    expect(resources.filter(item => item.path.endsWith('resource_observations')).length).toBe(offCount)
    report.auto_refresh = { natural_timer: true, server_end_advanced: true, unchanged_url: true, off_restored: true, off_no_requests: true }
    await page.screenshot({ path: resolve(artifact, 'node-resources-restored.png'), animations: 'disabled' })
    save('filesystem-mount-trend-restore')
    const filesystemTable = monitor.getByTestId('resource-filesystem-table')
    await expect(filesystemTable.getByRole('button', { name: '查看趋势' }).first()).toBeVisible()
    const mounts = resources.filter(item => item.path.endsWith('resource_observations') && item.query.metrics.split(',').includes('node.filesystem.total_bytes')).at(-1)
    expect(mounts.query.metrics.split(',').sort()).toEqual(filesystemMetrics.map(item => item.key).sort())
    expect(mounts.value.series.every(item => Object.keys(item.dimensions).length === 3)).toBe(true)
    const mountInodes = resources.filter(item => item.path.endsWith('resource_observations') && item.query.metrics.split(',').includes('node.filesystem.inodes_total')).at(-1)
    const observedMounts = resourceDimensionRows(mounts.value, mountInodes.value)
    const supportedMountIndex = observedMounts.findIndex(row => [...filesystemMetrics, ...inodeMetrics].every(metric => row.metrics[metric.key]?.data_state === 'valid'))
    expect(supportedMountIndex).toBeGreaterThanOrEqual(0)
    const supportedMount = observedMounts[supportedMountIndex].dimensions
    await filesystemTable.locator('.el-table__body tr').nth(supportedMountIndex).getByRole('button', { name: '查看趋势' }).click()
    await expect(page).toHaveURL(url => url.searchParams.get('metric') === 'node.filesystem.used_percent' && Boolean(url.searchParams.get('mountpoint')) && Boolean(url.searchParams.get('device')) && Boolean(url.searchParams.get('fstype')))
    await expect(monitor.getByTestId('resource-selected-mount')).toBeVisible()
    await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
    await page.reload()
    await expect(monitor.getByTestId('resource-selected-mount')).toBeVisible()
    await expect(monitor.getByTestId('resource-metric')).toContainText('可用容量使用率')
    await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
    await Promise.all(pending)
    const selected = Object.fromEntries(new URL(page.url()).searchParams)
    for (const key of ['device', 'mountpoint', 'fstype']) expect(selected[key]).toBe(supportedMount[key])
    const filesystemTrend = resources.filter(item => item.query.metrics === 'node.filesystem.used_percent').at(-1)
    for (const key of ['device', 'mountpoint', 'fstype']) expect(filesystemTrend.query[key]).toBe(selected[key])
    expect(filesystemTrend.value.series).toHaveLength(1)
    expect(filesystemTrend.value.series[0].points.some(point => point.data_state === 'valid')).toBe(true)
    await monitor.getByTestId('resource-filesystems').scrollIntoViewIfNeeded()
    await page.screenshot({ path: resolve(artifact, 'node-resources-filesystem.png'), animations: 'disabled' })
    save('inode-mount-trend-restore')
    const inodeReply = resources.filter(item => item.path.endsWith('resource_observations') && item.query.metrics.split(',').includes('node.filesystem.inodes_total')).at(-1)
    expect(inodeReply.query.metrics.split(',').sort()).toEqual(inodeMetrics.map(item => item.key).sort())
    const supported = inodeReply.value.series.find(item => item.metric_key === 'node.filesystem.inodes_total' && ['device', 'mountpoint', 'fstype'].every(key => item.dimensions[key] === selected[key]))
    expect(supported.points[0].data_state).toBe('valid')
    await monitor.getByTestId('resource-metric').click()
    await monitor.getByRole('option', { name: 'inode 使用率', exact: true }).click()
    await expect(page).toHaveURL(url => url.searchParams.get('metric') === 'node.filesystem.inodes_used_percent')
    await page.reload()
    await expect(monitor.getByTestId('resource-metric')).toContainText('inode 使用率')
    await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
    await Promise.all(pending)
    const inodeTrend = resources.filter(item => item.query.metrics === 'node.filesystem.inodes_used_percent').at(-1)
    for (const key of ['device', 'mountpoint', 'fstype']) expect(inodeTrend.query[key]).toBe(selected[key])
    expect(inodeTrend.value.series).toHaveLength(1)
    expect(inodeTrend.value.series[0].points.some(point => point.data_state === 'valid')).toBe(true)
    await monitor.getByTestId('resource-chart').scrollIntoViewIfNeeded()
    await page.screenshot({ path: resolve(artifact, 'node-resources-inodes.png'), animations: 'disabled' })
    for (const [family, metrics] of [['disk', diskMetrics], ['network', networkMetrics]]) {
      save(`${family}-device-trend-restore`)
      const table = monitor.getByTestId(`resource-${family}-table`)
      await expect(table.getByRole('button', { name: '查看趋势' }).first()).toBeVisible()
      await Promise.all(pending)
      const deviceReply = resources.filter(item => item.path.endsWith('resource_observations') && item.query.metrics.split(',').includes(metrics[0].key)).at(-1)
      expect(deviceReply.query.metrics.split(',').sort()).toEqual(metrics.map(item => item.key).sort())
      const device = resourceDimensionRows(deviceReply.value).find(row => metrics.filter(metric => metric.unit !== 'milliseconds').every(metric => row.metrics[metric.key]?.data_state === 'valid'))?.dimensions.device
      expect(device).toBeTruthy()
      const deviceRows = table.locator('.el-table__body tr')
      const deviceNames = await deviceRows.locator('td:first-child').allTextContents()
      const deviceIndex = deviceNames.findIndex(name => name.trim() === device)
      expect(deviceIndex).toBeGreaterThanOrEqual(0)
      const deviceRow = deviceRows.nth(deviceIndex)
      await expect(deviceRow).toHaveCount(1)
      const rateValues = deviceRow.locator('strong')
      await expect(rateValues).toHaveCount(metrics.length)
      for (let index = 0; index < 2; index++) await expect(rateValues.nth(index)).toHaveText(/\d[\d,.]* (?:B|KiB|MiB|GiB|TiB|PiB)\/s$/)
      report.presentation[`${family}_rate_units`] = true
      if (family === 'disk') {
        await expect(rateValues.nth(2)).toHaveText(/\d[\d,.]* %$/)
        for (const index of [3, 4]) await expect(rateValues.nth(index)).toHaveText(/^(?:—|\d[\d,.]* ms)$/)
        report.presentation.disk_timing_units = true
      }
      await deviceRow.getByRole('button', { name: '查看趋势' }).click()
      await expect(page).toHaveURL(url => url.searchParams.get('metric') === metrics[0].key && url.searchParams.get('device') === device && !url.searchParams.has('mountpoint') && !url.searchParams.has('fstype'))
      for (const metric of metrics) {
        if (metric !== metrics[0]) {
          await monitor.getByTestId('resource-metric').click()
          await monitor.getByRole('option', { name: ({diskRead: '磁盘读取吞吐', diskWrite: '磁盘写入吞吐', diskBusy: 'IO 忙碌时间占比', diskReadDuration: '平均读取耗时', diskWriteDuration: '平均写入耗时', networkReceive: '网络接收吞吐', networkTransmit: '网络发送吞吐'})[metric.name], exact: true }).click()
        }
        await page.reload()
        await expect(page).toHaveURL(url => url.searchParams.get('metric') === metric.key && url.searchParams.get('device') === device && url.searchParams.get('range') === '5m' && url.searchParams.get('refresh') === 'off' && !url.searchParams.has('mountpoint') && !url.searchParams.has('fstype'))
        await expect(monitor.getByTestId('resource-selected-device')).toHaveText(device)
        await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
        await Promise.all(pending)
        const deviceTrend = resources.filter(item => item.query.metrics === metric.key).at(-1)
        expect(deviceTrend.query.device).toBe(device)
        expect(deviceTrend.value.series).toHaveLength(1)
        expect(deviceTrend.value.series[0].dimensions).toEqual({ device })
        if (metric.unit !== 'milliseconds') expect(deviceTrend.value.series[0].points.some(point => point.data_state === 'valid')).toBe(true)
      }
      await monitor.getByTestId('resource-chart').scrollIntoViewIfNeeded()
      await page.screenshot({ path: resolve(artifact, `node-resources-${family}s.png`), animations: 'disabled' })
    }
    expect(businessErrors).toEqual([])
    report.resources = resources
    report.navigation = { list_without_fanout: true, iframe_preserved: true, history: true, metric_reload: true, range_reload: true, server_window: true, filesystem_reload: true, inode_reload: true, disk_reload: true, network_reload: true }
    save('security-administrator-denied')
    const negativeContext = await browser.newContext({ baseURL: process.env.CONSOLE_URL, locale: 'zh-CN' })
    try {
      const negative = await negativeContext.newPage()
      let nodeReads = 0
      negative.on('request', request => { if (/\/api\/v1\/(system\/platform\/host_nodes|monitor\/platform\/resource_)/.test(new URL(request.url()).pathname)) nodeReads++ })
      report.negative_identity = await login(negative, 'ADDP_ONLINE_METRICS_SECURITY', expected.security_id, 'platform.security_administrator')
      await expect(negative.locator('.el-result')).toBeVisible()
      await expect(negative.locator('iframe[data-testid="module-iframe"]')).toHaveCount(0)
      await expect(negative.locator('.sidebar .el-menu-item').filter({ hasText: '主机监控' })).toHaveCount(0)
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
