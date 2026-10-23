import { expect, test } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { readFileSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { hostListMetrics, resourceMetrics, filesystemMetrics, inodeMetrics, diskMetrics, networkMetrics, resourceDimensionRows, resourceTrendComparison, resourceTrendMetrics } from '../../../../monitor/frontend/src/utils/nodeResources.js'
import { formatFieldPresentationValue } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'
import { processMetrics, validateProcessInstances, validateProcessSummaries, validateProcessTrend } from '../../../../monitor/frontend/src/utils/processResources.js'
import { validateResourceOverview } from '../../../../monitor/frontend/src/utils/resourceOverview.js'
import { json, matchesRedirectURL } from './transfer-browser-support.js'
import { scalarResourceReceipts } from './node-resource-receipts.js'
import { engineMetrics, engineSummaryMetrics, validateEngineResources } from '../../../../monitor/frontend/src/utils/engineResources.js'

// Login failures must never capture MFA input or credentials.
test.use({ screenshot: 'off', trace: 'off' })

test('platform node resources through real Console password MFA and Monitor iframe', async ({ browser }, testInfo) => {
  test.setTimeout(240_000)
  const artifact = process.env.ADDP_ONLINE_ARTIFACT_DIR
  const expected = JSON.parse(readFileSync(resolve(artifact, 'node-resources-browser-input.json'), 'utf8'))
  const repository = resolve(process.cwd(), '../..')
  const report = { schema_version: 'addp.node-resources-browser/v1', result: 'failed', run_id: expected.run_id }
  const browserStarted = performance.now()
  let stageStarted = browserStarted
  const save = stage => {
    const now = performance.now()
    if (report.stage !== stage) stageStarted = now
    report.timing = { elapsed_ms: Math.round(now - browserStarted), stage_elapsed_ms: Math.round(now - stageStarted), test_timeout_ms: testInfo.timeout }
    report.stage = stage
    writeFileSync(resolve(artifact, 'node-resources-browser.json'), JSON.stringify(report, null, 2))
  }
  const context = await browser.newContext({ baseURL: process.env.CONSOLE_URL, locale: 'zh-CN', viewport: { width: 1440, height: 900 } })
  const page = await context.newPage()
  const resources = []
  const summaries = []
  const businessErrors = []
  const pending = []
  const instancePages = []
  const serviceBatches = []
  const serviceTrends = []
  const overviews = []
  const engineReads = []
  page.on('response', response => {
    if (expected.engine_resources) {
      const url = new URL(response.url())
      if (/^\/api\/v1\/monitor\/platform\/engine_resource_(observations|summaries|trends)$/.test(url.pathname)) {
        pending.push(response.json().then(value => engineReads.push({ path: url.pathname, query: Object.fromEntries(url.searchParams), value, status: response.status() })))
      }
      return
    }
    // Admission records its own write receipts; resource polling belongs to the resource cases.
    if (expected.admission || expected.engine_admission) return
    const url = new URL(response.url())
    if (url.pathname === '/api/v1/system/platform/module-instances') {
      pending.push(response.json().then(value => instancePages.push({ query: Object.fromEntries(url.searchParams), value })))
      return
    }
    if (url.pathname === '/api/v1/monitor/platform/process_resource_summaries') {
      pending.push(response.json().then(value => serviceBatches.push({ path: url.pathname, query: Object.fromEntries(url.searchParams), value })))
      return
    }
    if (url.pathname === '/api/v1/monitor/platform/process_resource_trends') {
      pending.push(response.json().then(value => serviceTrends.push({ path: url.pathname, query: Object.fromEntries(url.searchParams), value })))
      return
    }
    if (!/^\/api\/v1\/(system\/platform\/host_nodes|monitor\/platform\/resource_)/.test(url.pathname)) return
    if (!response.ok()) businessErrors.push(response.status())
    if (url.pathname.endsWith('resource_observations') || url.pathname.endsWith('resource_trends')) {
      pending.push(response.json().then(value => resources.push({ path: url.pathname, query: Object.fromEntries(url.searchParams), value })))
    }
    if (url.pathname.endsWith('resource_summaries')) {
      pending.push(response.json().then(value => summaries.push({ path: url.pathname, query: Object.fromEntries(url.searchParams), value })))
    }
    if (url.pathname.endsWith('resource_overview')) {
      pending.push(response.json().then(value => overviews.push({ path: url.pathname, query: Object.fromEntries(url.searchParams), value })))
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
    if (expected.engine_resources) {
      const owners = expected.engine_resources.owners
      save('engine-professional-list')
      await page.goto('/monitor/engine-resources?refresh=off')
      await expect(monitor.getByTestId('engine-list').getByRole('button', { name: '查看监控', exact: true })).toHaveCount(2)
      for (const owner of owners) await expect(monitor.getByTestId('engine-list')).toContainText(owner.display_name)
      await page.screenshot({ path: resolve(artifact, 'engine-resources-list.png'), fullPage: true, animations: 'disabled' })
      for (const owner of owners) {
        save('engine-professional-detail')
        const path = '/monitor/engine-resources?refresh=off&deployment_id=' + owner.deployment_id
        await page.goto(path)
        await expect(monitor.getByTestId('engine-metrics').locator('strong')).toHaveCount(12)
        for (const metric of engineMetrics) await expect(monitor.getByTestId('engine-value-' + metric.name)).not.toHaveText('—')
        await expect(monitor.getByTestId('engine-value-total')).toHaveText(/^\d[\d,]* 个$/)
        await expect(monitor.getByTestId('engine-value-capacity')).toHaveText(/^\d[\d,.]* (?:MiB|GiB|TiB)$/)
        // No workload fabricates this zero: the owned databases have no deadlocks.
        await expect(monitor.getByTestId('engine-value-deadlocks')).toHaveText('0.00 次/秒')
        await expect(monitor.getByTestId('engine-trend').getByRole('img', { name: '每秒事务提交', exact: true })).toBeVisible()
        await expect(monitor.getByTestId('engine-trend')).toContainText('当前数据库容器')
        await expect(monitor.getByTestId('engine-metrics')).not.toContainText('/metrics')
        await page.reload()
        await expect(page).toHaveURL(new RegExp('deployment_id=' + owner.deployment_id + '$'))
        await expect(monitor.getByTestId('engine-value-total')).not.toHaveText('—')
        await expect(monitor.getByTestId('engine-trend').getByRole('img', { name: '每秒事务提交', exact: true })).toBeVisible()
      }
      await page.screenshot({ path: resolve(artifact, 'engine-resources-detail.png'), fullPage: true, animations: 'disabled' })
      await Promise.all(pending)
      for (const receipt of engineReads) {
        expect(receipt.status).toBe(200)
        const summary = receipt.path.endsWith('_summaries'), trend = receipt.path.endsWith('_trends')
        const subjects = summary ? owners : owners.filter(owner => owner.deployment_id === receipt.query.deployment_id)
        validateEngineResources(receipt.value, subjects, summary ? engineSummaryMetrics : trend ? [engineMetrics[8]] : engineMetrics, trend ? receipt.query : null)
      }
      save('engine-professional-permission-denied')
      const negativeContext = await browser.newContext({ baseURL: process.env.CONSOLE_URL, locale: 'zh-CN' })
      try {
        const negative = await negativeContext.newPage()
        let reads = 0
        negative.on('request', request => { if (/\/api\/v1\/(system\/platform\/engine_deployments|monitor\/platform\/engine_resource_)/.test(new URL(request.url()).pathname)) reads++ })
        report.negative_identity = await login(negative, 'ADDP_ONLINE_METRICS_SECURITY', expected.security_id, 'platform.security_administrator')
        await negative.goto('/monitor/engine-resources?deployment_id=' + owners[0].deployment_id)
        await expect(negative.locator('.el-result')).toBeVisible()
        await expect(negative.locator('iframe[data-testid="module-iframe"]')).toHaveCount(0)
        expect(reads).toBe(0)
      } finally { await negativeContext.close() }
      report.engine_resources = { reads: engineReads, reloaded: true, zero_and_units: true, denied: true }
      report.result = 'passed'; save('complete'); return
    }
    if (expected.engine_admission) {
      save('engine-discovery-and-batch-confirmation')
      const writes = [], input = expected.engine_admission
      page.on('request', request => {
        if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/v1/system/platform/engine_deployments') writes.push(request.postDataJSON())
      })
      await page.goto('/system/engine-deployments')
      const system = page.frameLocator('iframe[data-testid="module-iframe"]')
      const discovered = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/system/platform/engine_deployments/discovery' && response.request().method() === 'GET')
      await system.getByTestId('deployment-create').click()
      expect((await discovered).status()).toBe(200)
      await expect(system.getByTestId('deployment-file')).toHaveCount(0)
      for (const item of input.items) await system.getByRole('textbox', { name: `${item.scope} ${item.service}`, exact: true }).fill(item.display_name)
      await system.getByTestId('deployment-candidates').locator('.el-table__header .el-checkbox').click()
      await system.getByRole('dialog').getByRole('combobox', { name: '承载主机', exact: true }).press('Enter')
      await system.getByRole('option', { name: input.host.display_name, exact: true }).click()
      const submitted = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/system/platform/engine_deployments' && response.request().method() === 'POST')
      await system.getByTestId('deployment-save').click()
      const response = await submitted
      expect(response.status()).toBe(201)
      const payload = await json(response, 'engine batch admission')
      expect(payload.data).toHaveLength(2)
      await expect(system.getByRole('dialog')).toHaveCount(0)
      await page.screenshot({ path: resolve(artifact, 'engine-admission-saved.png'), fullPage: true })
      const first = payload.data[0]
      await system.getByTestId(`deployment-detail-${first.deployment_id}`).click()
      await expect(page).toHaveURL(new RegExp('/system/engine-deployments/' + first.deployment_id + '$'))
      await expect(system.getByTestId('deployment-name')).toHaveValue(first.display_name)
      await page.reload()
      await expect(system.getByTestId('deployment-name')).toHaveValue(first.display_name)
      await expect(system.getByRole('dialog')).toContainText('当前版本：1')
      const currentSources = []
      async function verifyCurrentSource(row) {
        const current = system.getByTestId('deployment-current-source')
        await expect(current).toBeVisible()
        await expect(current).toContainText('容器正在运行')
        const containerID = row.inventory.services[0].containers[0].container_id
        await expect(system.getByTestId('deployment-current-container')).toContainText(containerID)
        currentSources.push({ deployment_id: row.deployment_id, container_id: containerID })
      }
      await verifyCurrentSource(first)
      await page.screenshot({ path: resolve(artifact, 'engine-admission-restored.png'), fullPage: true, animations: 'disabled' })
      await system.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).click()
      for (const row of payload.data.slice(1)) {
        await system.getByTestId(`deployment-detail-${row.deployment_id}`).click()
        await expect(system.getByTestId('deployment-name')).toHaveValue(row.display_name)
        await verifyCurrentSource(row)
        await system.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).click()
      }
      await system.getByTestId('deployment-create').click()
      for (const scope of ['Infra', 'Business']) {
        const row = system.getByTestId('deployment-candidates').getByRole('row').filter({ hasText: `${scope} / postgres` })
        await expect(row).toContainText('已接入')
        await expect(row.locator('.el-checkbox')).toBeDisabled()
      }
      expect(writes).toHaveLength(1)
      report.engine_admission = { writes, created: payload.data, reloaded: true, registered_disabled: true, current_sources: currentSources }
      report.result = 'passed'; save('complete'); return
    }
    if (expected.admission) {
      save('host-admission-confirm-and-retry')
      const writes = []
      page.on('request', request => {
        const path = new URL(request.url()).pathname
        if (request.method() === 'POST' && ['/api/v1/system/platform/host_nodes', '/api/v1/monitor/platform/monitoring_targets'].includes(path)) {
          writes.push({ path, input: request.postDataJSON() })
        }
      })
      await page.goto('/system/host-nodes')
      const system = page.frameLocator('iframe[data-testid="module-iframe"]')
      await system.getByRole('button', { name: '从部署清单接入', exact: true }).click()
      await expect(page).toHaveURL(/\/system\/host-admission$/)
      await system.getByTestId('admission-file').setInputFiles({ name: 'host-admission.json', mimeType: 'application/json', buffer: Buffer.from(JSON.stringify(expected.admission)) })
      await expect(system.getByTestId('host-admission')).toContainText(expected.admission.host.display_name)
      await system.getByText('按清单新建主机', { exact: true }).click()
      const hostCreated = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/system/platform/host_nodes' && response.request().method() === 'POST')
      await system.getByTestId('admission-confirm').click()
      const hostResponse = await hostCreated
      expect(hostResponse.status()).toBe(201)
      const host = await json(hostResponse, 'admission host creation')
      await expect(system.getByTestId('admission-created')).toContainText(host.node_id)
      await expect(system.getByTestId('admission-deployment')).toHaveValue(`ADDP_HOST_NODE_ID=${host.node_id}`)
      await system.getByTestId('admission-collect').click()
      await expect(monitor.getByTestId('target-endpoint')).toHaveValue(expected.admission.collection.source.endpoint)
      expect(page.url()).not.toContain(expected.admission.collection.source.endpoint)
      const invalidEndpoint = expected.admission.collection.source.endpoint.replace(/\/metrics$/, '/rejected-path')
      await monitor.getByTestId('target-endpoint').fill(invalidEndpoint)
      const rejected = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/monitor/platform/monitoring_targets' && response.request().method() === 'POST')
      await monitor.getByTestId('target-save').click()
      const failed = await rejected
      expect(failed.status()).toBe(400)
      const failure = await failed.json()
      expect(failure.error_code).toBe('invalid_monitoring_target')
      await expect(monitor.getByTestId('target-node')).toContainText(host.display_name)
      await expect(monitor.getByTestId('target-save-error')).toBeVisible()
      await page.screenshot({ path: resolve(artifact, 'node-admission-partial.png'), animations: 'disabled' })
      await monitor.getByTestId('target-endpoint').fill(expected.admission.collection.source.endpoint)
      const targetCreated = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/monitor/platform/monitoring_targets' && response.request().method() === 'POST')
      await monitor.getByTestId('target-save').click()
      const targetResponse = await targetCreated
      expect(targetResponse.status()).toBe(201)
      const target = await json(targetResponse, 'admission target creation')
      await expect(page).toHaveURL(new RegExp(`/monitor/monitoring-targets/${target.id}$`))
      await expect(monitor.getByTestId('target-endpoint')).toHaveValue(expected.admission.collection.source.endpoint)
      // Wait until the persisted detail is editable before reloading it.
      await expect(monitor.getByTestId('target-save')).toBeEnabled()
      await page.reload()
      await expect(monitor.getByTestId('target-endpoint')).toHaveValue(expected.admission.collection.source.endpoint)
      await expect(monitor.getByTestId('target-save')).toBeEnabled()
      await page.screenshot({ path: resolve(artifact, 'node-admission-saved.png'), animations: 'disabled' })
      expect(writes.map(row => row.path)).toEqual(['/api/v1/system/platform/host_nodes', '/api/v1/monitor/platform/monitoring_targets', '/api/v1/monitor/platform/monitoring_targets'])
      const facts = (value, keys) => Object.fromEntries(keys.map(key => [key, value[key]]))
      report.admission = { host: facts(host, ['node_id', 'version', 'display_name', 'node_kind', 'addresses', 'allowed_module_bindings', 'enabled']),
        target: facts(target, ['id', 'version', 'subject', 'monitor_kind', 'source', 'enabled']),
        writes, failure: { status: failed.status(), error_code: failure.error_code }, reloaded: true }
      report.result = 'passed'
      save('complete')
      return
    }
    if (expected.module_workers) {
      save('module-worker-resources-and-host')
      const { module, workers, host } = expected.module_workers
      const path = `/monitor/service-resources?module_name=${encodeURIComponent(module)}&role=worker&refresh=off`
      const checkList = async (after) => {
        await expect(monitor.getByTestId('process-list')).toBeVisible()
        await expect(monitor.getByTestId('process-list').locator('.el-table__body tr')).toHaveCount(workers.length)
        for (const owner of workers) await expect(monitor.getByTestId('process-list')).toContainText(owner.instance_id)
        await expect.poll(async () => {
          await Promise.all(pending)
          return serviceBatches.slice(after).some(item => item.query.instance_ids?.split(',').sort().join(',') === workers.map(row => String(row.id)).sort().join(','))
        }).toBe(true)
        const batch = serviceBatches.slice(after).filter(item => item.query.instance_ids?.split(',').sort().join(',') === workers.map(row => String(row.id)).sort().join(',')).at(-1)
        const values = validateProcessSummaries(batch.value, workers)
        for (const owner of workers) {
          expect(values.get(owner.id).node_id).toBe(host.node_id)
          expect(values.get(owner.id).series.every(line => line.points[0].data_state === 'valid')).toBe(true)
        }
        return batch
      }
      let batchStart = serviceBatches.length
      await page.goto(path)
      await checkList(batchStart)
      batchStart = serviceBatches.length
      await page.reload()
      const summary = await checkList(batchStart)
      await page.screenshot({ path: resolve(artifact, 'module-workers-list.png'), animations: 'disabled' })
      const trends = []
      for (const owner of workers) {
        await page.goto(path + '&instance_id=' + owner.id)
        await expect(monitor.getByTestId('process-trend')).toContainText(owner.instance_id)
        for (const metric of ['cpu', 'memory']) await expect(monitor.getByTestId(`process-trend-${metric}`).locator('canvas')).toBeVisible()
        await expect.poll(async () => {
          await Promise.all(pending)
          return serviceTrends.some(item => item.query.instance_id === String(owner.id))
        }).toBe(true)
        const trend = serviceTrends.filter(item => item.query.instance_id === String(owner.id)).at(-1)
        validateProcessTrend(trend.value, owner, trend.query)
        for (const line of trend.value.series) expect(line.points.some(point => point.data_state === 'valid')).toBe(true)
        trends.push(trend)
      }
      await page.screenshot({ path: resolve(artifact, 'module-workers-trends.png'), animations: 'disabled' })
      batchStart = serviceBatches.length
      await page.goto(path)
      await checkList(batchStart)
      await monitor.getByTestId('process-host').getByRole('button', { name: host.display_name, exact: true }).first().click()
      await expect(page).toHaveURL(new RegExp(`/monitor/node-resources/${host.node_id}$`))
      await expect(monitor.getByRole('heading', { name: host.display_name, exact: true })).toBeVisible()
      batchStart = serviceBatches.length
      await page.goBack()
      await expect(page).toHaveURL(url => matchesRedirectURL(url, new URL(path, process.env.CONSOLE_URL)))
      await checkList(batchStart)
      await Promise.all(pending)
      expect(businessErrors).toEqual([])
      report.module_workers = { summary, trends, reload_restored: true, host_navigation: true }
      report.result = 'passed'
      save('complete')
      return
    }
    if (expected.binding) {
      save('bound-service-host-navigation')
      const { host, instance, target } = expected.binding
      const servicePath = '/monitor/service-resources?module_name=monitor&role=backend&refresh=off'
      await page.goto(servicePath)
      await expect(monitor.getByTestId('process-list')).toBeVisible()
      const link = monitor.getByTestId('process-host').getByRole('button', { name: host.display_name, exact: true })
      await expect(link).toBeVisible()
      await expect(monitor.getByTestId('process-list').locator('.el-table__body tr')).toHaveCount(1)
      await expect(monitor.getByTestId('process-list')).toContainText('正在采集')
      await expect.poll(async () => {
        await Promise.all(pending)
        return serviceBatches.some(row => row.query.instance_ids === String(instance.id))
      }).toBe(true)
      const batch = serviceBatches.filter(row => row.query.instance_ids === String(instance.id)).at(-1)
      const observation = validateProcessSummaries(batch.value, [instance]).get(instance.id)
      expect(observation.node_id).toBe(host.node_id)
      expect(observation.series.every(series => series.points[0].data_state === 'valid')).toBe(true)
      const list = instancePages.filter(row => row.query.module_name === 'monitor' && row.query.status === 'up').at(-1)
      expect(list.value.total).toBe(1)
      expect(list.value.data[0]).toMatchObject({ id: instance.id, instance_id: instance.instance_id, node_id: host.node_id,
        declared_node_id: host.node_id, node_binding_state: 'bound', node_binding_reason: '' })
      const serviceURL = page.url()
      const origin = await monitor.locator('body').evaluate(() => performance.timeOrigin)
      await page.screenshot({ path: resolve(artifact, 'node-binding-services.png'), animations: 'disabled' })
      await link.click()
      await expect(page).toHaveURL(new RegExp(`/monitor/node-resources/${host.node_id}$`))
      await expect(monitor.getByRole('heading', { name: host.display_name, exact: true })).toBeVisible()
      await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
      await expect.poll(async () => {
        await Promise.all(pending)
        return scalarResourceReceipts(resources, host.node_id).length > 0
      }).toBe(true)
      const resource = scalarResourceReceipts(resources, host.node_id).at(-1)
      expect(resource.value.node_version).toBe(host.version)
      expect(resource.value.target_id).toBe(target.id)
      expect(resource.value.collection.state).toBe('collecting')
      expect(resource.value.series.map(series => series.metric_key).sort()).toEqual(resourceMetrics.map(metric => metric.key).sort())
      expect(resource.value.series.every(series => series.points[0].data_state === 'valid')).toBe(true)
      for (const metric of resourceMetrics) {
        const card = monitor.getByTestId(`resource-${metric.name}`)
        await expect(card).toBeVisible()
        await expect(card).toContainText('有效')
        await expect(card.locator('strong')).not.toHaveText('—')
      }
      expect(await monitor.locator('body').evaluate(() => performance.timeOrigin)).toBe(origin)
      await page.screenshot({ path: resolve(artifact, 'node-binding-resources.png'), animations: 'disabled' })
      await page.goBack()
      await expect(page).toHaveURL(serviceURL)
      await expect(link).toBeVisible()
      // History navigation is Console-owned; only the completed forward sync must keep its document.
      await expect(page.locator('iframe[data-testid="module-iframe"]')).toHaveCount(1)
      await expect(monitor.getByTestId('process-list').locator('.el-table__body tr')).toHaveCount(1)
      await page.reload()
      await expect(page).toHaveURL(serviceURL)
      await expect(link).toBeVisible()
      await Promise.all(pending)
      expect(businessErrors).toEqual([])
      report.binding = { host, instance, process_summary: batch, resources: resource, same_iframe: true, back_restored: true, reload_restored: true }
      report.result = 'passed'
      save('complete')
      return
    }
    await expect(monitor.getByTestId('resource-search')).toBeVisible()
    await expect(monitor.getByRole('button', { name: '查看资源' })).toHaveCount(1)
    await expect(monitor.getByTestId('resource-node-list')).toContainText('正在采集')
    await expect(monitor.getByTestId('resource-node-list').locator('strong')).toHaveCount(5)
    for (const metric of ['cpuBusy', 'memoryUsed']) await expect(monitor.getByTestId(`resource-summary-${metric}`)).toHaveText(/^\d[\d,]*\.\d{2} %$/)
    await expect(monitor.getByTestId('resource-summary-cores')).toHaveText(/^[\d,]+ 核$/)
    await expect(monitor.getByTestId('resource-summary-memoryTotal')).toHaveText(/[KMGT]iB$/)
    await expect(monitor.getByTestId('resource-summary-uptime')).not.toHaveText('—')
    await Promise.all(pending)
    expect(resources).toHaveLength(0)
    expect(summaries).toHaveLength(1)
    expect(summaries[0].query).toEqual({ node_ids: expected.node.node_id, metrics: hostListMetrics.map(metric => metric.key).join(',') })
    expect(summaries[0].value.data).toHaveLength(1)
    expect(summaries[0].value.data[0].series.map(row => row.metric_key).sort()).toEqual(hostListMetrics.map(row => row.key).sort())
    await page.screenshot({ path: resolve(artifact, 'node-resources-list.png'), animations: 'disabled' })
    const documentOrigin = () => monitor.locator('body').evaluate(() => performance.timeOrigin)
    const documentID = await documentOrigin()
    save('node-detail-catalog-metrics')
    await monitor.getByRole('button', { name: '查看资源' }).click()
    await expect(page).toHaveURL(new RegExp(`/monitor/node-resources/${expected.node.node_id}$`))
    await expect(monitor.getByRole('heading', { name: expected.display_name, exact: true })).toBeVisible()
    await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
    expect(await documentOrigin()).toBe(documentID)
    await Promise.all(pending)
    const instant = scalarResourceReceipts(resources, expected.node.node_id)[0]
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
      const anchor = scalarResourceReceipts(resources, expected.node.node_id).find(row => Date.parse(row.value.end) === Date.parse(item.query.end))
      expect(anchor).toBeTruthy()
      expect([300_000, 3_600_000]).toContain(Date.parse(item.query.end) - Date.parse(item.query.start))
    }
    await monitor.getByTestId('resource-metric').click()
    await monitor.getByRole('option', { name: 'TCP 当前连接数', exact: true }).click()
    await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
    await expect(monitor.getByTestId('resource-tcpConnections').locator('strong')).toHaveText(/^[\d,]+ 条$/)
    await expect(monitor.getByTestId('resource-tcpConnections')).toContainText('ESTABLISHED + CLOSE_WAIT')
    await Promise.all(pending)
    expect(resources.some(item => item.path.endsWith('resource_trends') && item.query.metrics === 'node.tcp.current_connections')).toBe(true)
    await page.screenshot({ path: resolve(artifact, 'node-resources-tcp.png'), animations: 'disabled' })
    await monitor.getByTestId('resource-metric').click()
    await monitor.getByRole('option', { name: 'CPU 忙碌率（1 分钟）', exact: true }).click()
    await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
    save('natural-auto-refresh-and-off-restore')
    const beforeAuto = scalarResourceReceipts(resources, expected.node.node_id).length
    const priorEnd = scalarResourceReceipts(resources, expected.node.node_id).at(-1).value.end
    const refreshURL = page.url()
    await expect(monitor.getByTestId('resource-refresh')).toContainText('每 15 秒')
    const autoInstant = page.waitForResponse(response => {
      const url = new URL(response.url())
      return response.ok() && scalarResourceReceipts([{ path: url.pathname, query: Object.fromEntries(url.searchParams) }], expected.node.node_id).length === 1
    })
    const autoTrend = page.waitForResponse(response => new URL(response.url()).pathname.endsWith('resource_trends') && response.ok())
    await autoInstant
    await autoTrend
    await Promise.all(pending)
    expect(scalarResourceReceipts(resources, expected.node.node_id).length).toBeGreaterThan(beforeAuto)
    expect(Date.parse(scalarResourceReceipts(resources, expected.node.node_id).at(-1).value.end)).toBeGreaterThan(Date.parse(priorEnd))
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
    save('adjacent-resource-period-comparison')
    report.period_comparisons = []
    const comparePeriod = async (phase, metric, action) => {
      await Promise.all(pending)
      const before = resources.length
      await action()
      await expect(monitor.getByTestId('resource-comparison')).toBeVisible()
      await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
      await expect(monitor.getByTestId('resource-compare').getByRole('checkbox')).toBeChecked()
      await expect(monitor.getByTestId('resource-refresh')).toContainText('关闭自动刷新')
      await expect(page).toHaveURL(url => url.pathname === `/monitor/node-resources/${expected.node.node_id}` && url.searchParams.get('compare') === 'previous' && url.searchParams.get('range') === '5m' && url.searchParams.get('refresh') === 'off' && (url.searchParams.get('metric') || 'node.memory.used_percent') === metric)
      await Promise.all(pending)
      const batch = resources.slice(before), curves = batch.filter(row => row.path.endsWith('resource_trends'))
      expect(batch).toHaveLength(6)
      expect(curves).toHaveLength(1)
      const curve = curves[0], statistics = resourceTrendComparison(curve.value, '5m')
      expect(curve.query.metrics).toBe(resourceTrendMetrics(metric).map(item => item.key).join(','))
      expect(Date.parse(curve.query.end) - Date.parse(curve.query.start)).toBe(600_000)
      for (const period of ['current', 'previous']) {
        const card = monitor.getByTestId(`resource-compare-${period}`)
        await expect(card.locator('strong')).toHaveText(formatFieldPresentationValue(statistics[period].mean, { precision: 2, unit: '%' }, 'zh-cn'))
        await expect(card).toContainText(`有效评估点：${statistics[period].valid} / ${statistics[period].total}`)
      }
      if (statistics.complete) {
        await expect(monitor.getByTestId('resource-compare-change')).toBeVisible()
        await expect(monitor.getByTestId('resource-compare-incomplete')).toHaveCount(0)
        const delta = statistics.delta
        const change = delta === 0 ? '与上期持平' : Math.round(Math.abs(delta) * 100) === 0 ? '较上期变化不足 0.01 个百分点' : `较上期${delta > 0 ? '上升' : '下降'} ${formatFieldPresentationValue(Math.abs(delta), { precision: 2 }, 'zh-cn')} 个百分点`
        await expect(monitor.getByTestId('resource-compare-change')).toHaveText(change)
      } else {
        await expect(monitor.getByTestId('resource-compare-incomplete')).toContainText('样本不足，暂不比较')
        await expect(monitor.getByTestId('resource-compare-change')).toHaveCount(0)
      }
      await monitor.getByTestId('resource-comparison').scrollIntoViewIfNeeded()
      await page.screenshot({ path: resolve(artifact, `node-resources-comparison-${phase}.png`), animations: 'disabled' })
      report.period_comparisons.push({ phase, before_index: before, resource_index: resources.indexOf(curve),
        page_path: new URL(page.url()).pathname, page_query: Object.fromEntries(new URL(page.url()).searchParams), statistics })
    }
    await comparePeriod('cpu', 'node.cpu.busy_percent', () => monitor.getByTestId('resource-compare').check())
    await comparePeriod('memory', 'node.memory.used_percent', async () => {
      await monitor.getByTestId('resource-metric').click()
      await monitor.getByRole('option', { name: '内存使用率', exact: true }).click()
    })
    await comparePeriod('restored', 'node.memory.used_percent', () => page.reload())
    expect(report.period_comparisons[2].page_query).toEqual(report.period_comparisons[1].page_query)
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
      if (family === 'disk') {
        const expand = monitor.getByTestId('resource-disk-toggle')
        if (await expand.count()) {
          await expect(expand).toHaveAttribute('aria-expanded', 'false')
          await expand.click()
          await expect(expand).toHaveAttribute('aria-expanded', 'true')
        }
      }
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
    report.summaries = summaries
    report.navigation = { list_summary_batch: true, iframe_preserved: true, history: true, metric_reload: true, range_reload: true, server_window: true, filesystem_reload: true, inode_reload: true, disk_reload: true, network_reload: true }
    save('service-monitor-inline-values-and-restore')
    const serviceMenu = page.locator('.sidebar .el-menu-item').filter({ hasText: /^服务监控$/ })
    if (!await serviceMenu.isVisible()) await page.locator('.sidebar .el-sub-menu__title').filter({ hasText: /^运行监控$/ }).click()
    await expect(serviceMenu).toBeVisible()
    await serviceMenu.click()
    await expect(page).toHaveURL(url => url.pathname === '/monitor/service-resources')
    await expect(monitor.getByTestId('process-list')).toBeVisible()
    await expect(page.locator('iframe[data-testid="module-iframe"]')).toHaveCount(1)
    const serviceDocumentID = await documentOrigin()
    await monitor.getByTestId('process-refresh').click()
    await monitor.getByRole('option', { name: '关闭自动刷新', exact: true }).click()
    await expect(page).toHaveURL(url => url.searchParams.get('refresh') === 'off')
    await expect(monitor.getByRole('button', { name: '刷新', exact: true })).toBeEnabled()
    // Keep the actual page controls and URL contract; do not intercept any API.
    const roleControl = monitor.locator('.el-select:has(input[aria-label="实例角色"])')
    const statusControl = monitor.locator('.el-select:has(input[aria-label="在线状态"])')
    await roleControl.click()
    await monitor.getByRole('option', { name: '后端服务', exact: true }).click()
    await statusControl.click()
    await monitor.getByRole('option', { name: '在线', exact: true }).click()
    await monitor.getByRole('button', { name: '搜索', exact: true }).click()
    await expect(page).toHaveURL(url => url.pathname === '/monitor/service-resources' && url.searchParams.get('role') === 'backend' && !url.searchParams.has('status') && !url.searchParams.has('process_metrics_declared'))
    expect(await documentOrigin()).toBe(serviceDocumentID)
    await expect(monitor.getByTestId('process-list').locator('strong').filter({ hasText: /MiB|GiB/ })).toHaveCount(2)
    for (const hint of ['1 核 ≈ 占满一个逻辑核，可超过 1 核', '进程当前驻留内存（RSS）', '当前进程启动后经过的时间']) {
      await expect(monitor.getByText(hint, { exact: true })).toBeVisible()
    }
    const checkServices = async () => {
      await Promise.all(pending)
      const list = instancePages.filter(item => item.query.role === 'backend' && item.query.status === 'up' && item.query.process_metrics_declared === 'true').at(-1)
      validateProcessInstances(list.value, 1, list.query)
      const batch = serviceBatches.filter(item => item.query.instance_ids === list.value.data.map(row => row.id).join(',')).at(-1)
      const summaries = validateProcessSummaries(batch.value, list.value.data)
      for (const owner of expected.processes) {
        const index = list.value.data.findIndex(item => item.id === owner.id && item.instance_id === owner.instance_id)
        expect(index).toBeGreaterThanOrEqual(0)
        const row = monitor.getByTestId('process-list').locator('.el-table__body tr').nth(index)
        await expect(row).toContainText('未关联主机')
        await expect(row).toContainText('正在采集')
        await expect(row.getByText(/^采样时间:/)).toBeVisible()
        const values = row.locator('strong')
        await expect(values.nth(0)).toHaveText(/^\d[\d,]*\.\d{2} 核$/)
        await expect(values.nth(1)).toHaveText(/^\d[\d,.]* (?:MiB|GiB)$/)
        await expect(values.nth(2)).toHaveText(/\d+(?:天|小时|分钟|秒)/)
        expect(summaries.get(owner.id).series.map(s => s.metric_key).sort()).toEqual(processMetrics.map(m => m.key).sort())
        expect(summaries.get(owner.id).series.every(s => s.points[0].data_state === 'valid')).toBe(true)
      }
      return { ...batch, instances: list.value, instance_query: list.query }
    }
    const firstServices = await checkServices()
    await page.reload()
    await expect(roleControl).toContainText('后端服务')
    await expect(statusControl).toContainText('在线')
    await expect(monitor.getByTestId('process-list').locator('strong').filter({ hasText: /MiB|GiB/ })).toHaveCount(2)
    const restoredServices = await checkServices()
    report.service_batches = [firstServices, restoredServices]
    report.service_presentation = { cpu_cores: true, rss_iec: true, elapsed_uptime: true, inline_hints: true,
      sample_time_visible: true, unbound_host: true, url_restore: true, iframe_preserved: true, negative_no_reads: false }
    await page.screenshot({ path: resolve(artifact, 'node-resources-services.png'), animations: 'disabled' })
    save('current-process-trends-and-restore')
    const trendOwner = restoredServices.instances.data.find(row => row.id === expected.processes[0].id)
    const trendIndex = restoredServices.instances.data.findIndex(row => row.id === trendOwner.id)
    await monitor.getByTestId('process-list').locator('.el-table__body tr').nth(trendIndex).getByRole('button', { name: '查看趋势', exact: true }).click()
    for (const metric of ['cpu', 'memory']) await expect(monitor.getByTestId(`process-trend-${metric}`).locator('canvas')).toBeVisible()
    await page.reload()
    for (const metric of ['cpu', 'memory']) await expect(monitor.getByTestId(`process-trend-${metric}`).locator('canvas')).toBeVisible()
    await expect(page).toHaveURL(url => url.searchParams.get('instance_id') === String(trendOwner.id))
    await Promise.all(pending)
    expect(serviceTrends.length).toBe(2)
    for (const item of serviceTrends) {
      const observed = validateProcessTrend(item.value, trendOwner, item.query)
      expect(observed.series.every(line => line.points.some(point => point.data_state === 'valid'))).toBe(true)
    }
    report.service_trends = serviceTrends.slice()
    report.process_trend_presentation = { cpu_cores: true, rss_iec: true, same_current_instance: true, startup_bound: true, url_restore: true }
    await page.screenshot({ path: resolve(artifact, 'node-resources-process-trends.png'), animations: 'disabled' })
    const memoryTrend = monitor.getByTestId('process-trend-memory').locator('canvas')
    await memoryTrend.scrollIntoViewIfNeeded()
    await expect(memoryTrend).toBeInViewport()
    await page.screenshot({ path: resolve(artifact, 'node-resources-process-trends-memory.png'), animations: 'disabled' })
    save('service-natural-auto-refresh-and-off-restore')
    await monitor.getByTestId('process-trend-refresh').click()
    await monitor.getByRole('option', { name: '每 15 秒', exact: true }).click()
    await expect(page).toHaveURL(url => !url.searchParams.has('refresh'))
    await expect(monitor.getByTestId('process-trend-refresh')).toContainText('每 15 秒')
    await expect(monitor.getByTestId('process-trend').getByRole('button', { name: '刷新', exact: true })).toBeEnabled()
    await Promise.all(pending)
    const beforeServiceAuto = serviceTrends.at(-1), beforeAutoBatch = await checkServices()
    const autoURL = page.url(), autoDocumentID = await documentOrigin(), beforeAutoCount = serviceTrends.length
    // Real wall-clock timer; no fake clock, manual reload or intercepted business API.
    await expect.poll(() => serviceTrends.length, { timeout: 25_000 }).toBe(beforeAutoCount + 1)
    await expect(monitor.getByTestId('process-trend').getByRole('button', { name: '刷新', exact: true })).toBeEnabled()
    const afterAutoBatch = await checkServices(), afterAuto = serviceTrends.at(-1)
    expect(Date.parse(afterAuto.value.end)).toBeGreaterThan(Date.parse(beforeServiceAuto.value.end))
    expect(page.url()).toBe(autoURL)
    expect(await documentOrigin()).toBe(autoDocumentID)
    for (const entry of [beforeServiceAuto, afterAuto]) validateProcessTrend(entry.value, trendOwner, entry.query)
    await monitor.getByTestId('process-trend-refresh').click()
    await monitor.getByRole('option', { name: '关闭自动刷新', exact: true }).click()
    await expect(page).toHaveURL(url => url.searchParams.get('refresh') === 'off')
    await expect(monitor.getByTestId('process-trend-refresh')).toContainText('关闭自动刷新')
    await expect(monitor.getByTestId('process-trend').getByRole('button', { name: '刷新', exact: true })).toBeEnabled()
    await Promise.all(pending)
    await page.reload()
    await expect(monitor.getByTestId('process-trend-memory').locator('canvas')).toBeVisible()
    await expect(monitor.getByTestId('process-trend-refresh-status')).toContainText('手工刷新')
    await Promise.all(pending)
    const offCounts = [instancePages.length, serviceBatches.length, serviceTrends.length]
    await new Promise(resolve => setTimeout(resolve, 16_000))
    await Promise.all(pending)
    expect([instancePages.length, serviceBatches.length, serviceTrends.length]).toEqual(offCounts)
    report.service_auto_refresh = { natural_timer: true, server_end_advanced: true, unchanged_url: true,
      iframe_preserved: true, off_restored: true, off_no_requests: true, batches: [beforeAutoBatch, afterAutoBatch], trends: [beforeServiceAuto, afterAuto] }
    // Closing the trend changes its URL and reloads the service list; finish that batch before navigating.
    const closedServiceBatch = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/monitor/platform/process_resource_summaries' && response.ok())
    await monitor.locator('.el-dialog__headerbtn').click()
    await closedServiceBatch
    await checkServices()
    save('host-list-natural-refresh-and-off-restore')
    await page.goto('/monitor/node-resources')
    await expect(monitor.getByTestId('resource-node-list')).toContainText('正在采集')
    await Promise.all(pending)
    const listBeforeIndex = summaries.length - 1
    const listBeforeEnd = summaries[listBeforeIndex].value.data[0].end
    const listURL = page.url(), listDocumentID = await documentOrigin(), listResourceCount = resources.length
    const listNext = page.waitForResponse(response => new URL(response.url()).pathname.endsWith('/resource_summaries') && response.ok())
    await listNext
    await Promise.all(pending)
    const listAfterIndex = summaries.length - 1
    expect(listAfterIndex).toBe(listBeforeIndex + 1)
    expect(Date.parse(summaries[listAfterIndex].value.data[0].end)).toBeGreaterThan(Date.parse(listBeforeEnd))
    expect(page.url()).toBe(listURL)
    expect(await documentOrigin()).toBe(listDocumentID)
    expect(resources).toHaveLength(listResourceCount)
    const listOff = page.waitForResponse(response => new URL(response.url()).pathname.endsWith('/resource_summaries') && response.ok())
    await monitor.getByTestId('resource-refresh').click()
    await monitor.getByRole('option', { name: '关闭自动刷新', exact: true }).click()
    await listOff
    await expect(page).toHaveURL(url => url.pathname === '/monitor/node-resources' && url.searchParams.get('refresh') === 'off')
    await page.reload()
    await expect(monitor.getByTestId('resource-node-list')).toContainText('正在采集')
    await expect(monitor.getByTestId('resource-refresh-status')).toContainText('手工刷新')
    await Promise.all(pending)
    const listOffCount = summaries.length
    await new Promise(resolve => setTimeout(resolve, 16_000))
    await Promise.all(pending)
    expect(summaries).toHaveLength(listOffCount)
    report.host_list_auto_refresh = { natural_timer: true, server_end_advanced: true, unchanged_url: true,
      iframe_preserved: true, off_restored: true, off_no_requests: true, before_index: listBeforeIndex, after_index: listAfterIndex }
    save('overview-natural-refresh-off-and-navigation')
    await page.goto('/monitor/resource-overview')
    await expect(monitor.getByTestId('overview-statistics')).toBeVisible()
    await expect(monitor.getByTestId('overview-refresh')).toContainText('每 15 秒')
    const checkOverview = async () => {
      await expect(monitor.getByTestId('resource-overview').getByRole('button', { name: '刷新', exact: true })).toBeEnabled()
      await Promise.all(pending)
      const entry = overviews.at(-1), value = validateResourceOverview(entry.value)
      expect(entry.query).toEqual({})
      expect(value.inventory).toEqual({ managed: 1, enabled: 1, disabled: 0, connected: 1, not_connected: 0,
        collection: { collecting: 1, failed: 0, stale: 0, no_sample: 0, not_connected: 0 } })
      for (const [index, rank] of value.rankings.entries()) {
        expect(rank.coverage).toEqual({ expected: 1, valid: 1, no_data: 0, stale: 0, not_connected: 0, complete: true })
        expect(rank.top).toHaveLength(1)
        expect(rank.top[0].node).toEqual({ node_id: expected.node.node_id, display_name: expected.display_name,
          node_kind: 'virtual', enabled: true, version: expected.node.version })
        await expect(monitor.getByTestId(`overview-rank-${index}`).getByRole('button', { name: expected.display_name, exact: true })).toBeVisible()
        await expect(monitor.getByTestId(`overview-rank-${index}`).locator('.el-table__body tr')).toHaveCount(1)
      }
      return entry
    }
    const beforeOverview = await checkOverview(), overviewURL = page.url(), overviewDocument = await documentOrigin()
    const overviewCount = overviews.length
    await expect.poll(() => overviews.length, { timeout: 25_000 }).toBe(overviewCount + 1)
    const afterOverview = await checkOverview()
    expect(Date.parse(afterOverview.value.evaluated_at)).toBeGreaterThan(Date.parse(beforeOverview.value.evaluated_at))
    expect(page.url()).toBe(overviewURL)
    expect(await documentOrigin()).toBe(overviewDocument)
    const overviewOffResponse = page.waitForResponse(response => new URL(response.url()).pathname.endsWith('/resource_overview') && response.ok())
    await monitor.getByTestId('overview-refresh').click()
    await monitor.getByRole('option', { name: '关闭自动刷新', exact: true }).click()
    await overviewOffResponse
    await expect(page).toHaveURL(url => url.pathname === '/monitor/resource-overview' && url.searchParams.get('refresh') === 'off')
    await checkOverview()
    await page.reload()
    await expect(monitor.getByTestId('overview-refresh-status')).toContainText('手工刷新')
    await checkOverview()
    const overviewOffCount = overviews.length
    await new Promise(resolve => setTimeout(resolve, 16_000))
    await Promise.all(pending)
    expect(overviews).toHaveLength(overviewOffCount)
    await page.screenshot({ path: resolve(artifact, 'node-resources-overview.png'), animations: 'disabled' })
    const overviewRestoredDocument = await documentOrigin()
    await monitor.getByTestId('overview-rank-0').getByRole('button', { name: expected.display_name, exact: true }).click()
    await expect(page).toHaveURL(url => url.pathname === `/monitor/node-resources/${expected.node.node_id}`)
    await expect(monitor.getByRole('heading', { name: expected.display_name, exact: true })).toBeVisible()
    await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
    expect(await documentOrigin()).toBe(overviewRestoredDocument)
    const overviewBackResponse = page.waitForResponse(response => new URL(response.url()).pathname.endsWith('/resource_overview') && response.ok())
    await page.goBack()
    await overviewBackResponse
    await expect(page).toHaveURL(url => url.pathname === '/monitor/resource-overview' && url.searchParams.get('refresh') === 'off')
    await checkOverview()
    await expect(monitor.getByTestId('overview-refresh-status')).toContainText('手工刷新')
    report.overview = { natural_timer: true, server_end_advanced: true, unchanged_url: true, iframe_preserved: true,
      off_restored: true, off_no_requests: true, detail_and_back: true, negative_no_reads: false, responses: overviews }
    expect(businessErrors).toEqual([])
    save('security-administrator-denied')
    const negativeContext = await browser.newContext({ baseURL: process.env.CONSOLE_URL, locale: 'zh-CN' })
    try {
      const negative = await negativeContext.newPage()
      let nodeReads = 0
      negative.on('request', request => { if (/\/api\/v1\/(system\/platform\/(host_nodes|module-instances)|monitor\/platform\/(resource_|process_resource_))/.test(new URL(request.url()).pathname)) nodeReads++ })
      report.negative_identity = await login(negative, 'ADDP_ONLINE_METRICS_SECURITY', expected.security_id, 'platform.security_administrator')
      await expect(negative.locator('.el-result')).toBeVisible()
      await expect(negative.locator('iframe[data-testid="module-iframe"]')).toHaveCount(0)
      await expect(negative.locator('.sidebar .el-menu-item').filter({ hasText: '主机监控' })).toHaveCount(0)
      await negative.goto('/monitor/service-resources')
      await expect(negative.locator('.el-result')).toBeVisible()
      await expect(negative.locator('iframe[data-testid="module-iframe"]')).toHaveCount(0)
      await expect(negative.locator('.sidebar .el-menu-item').filter({ hasText: '服务监控' })).toHaveCount(0)
      await negative.goto('/monitor/resource-overview')
      await expect(negative.locator('.el-result')).toBeVisible()
      await expect(negative.locator('iframe[data-testid="module-iframe"]')).toHaveCount(0)
      await expect(negative.locator('.sidebar .el-menu-item').filter({ hasText: '资源总览' })).toHaveCount(0)
      expect(nodeReads).toBe(0)
      report.overview.negative_no_reads = true
      report.service_presentation.negative_no_reads = true
      report.negative_no_business_reads = true
    } finally { await negativeContext.close() }
    report.result = 'passed'
    save('complete')
  } catch (error) {
    // Fixed route flags never export URL inputs or authentication state.
    const pathname = new URL(page.url()).pathname
    report.browser_state = { page_closed: page.isClosed(), current_node_list: pathname === '/monitor/node-resources', current_service_list: pathname === '/monitor/service-resources' }
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
