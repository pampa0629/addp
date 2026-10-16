import { expect, request, test } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { browserAPIFromResponse, identity, isAnonymousRefreshConsoleError, isAnonymousRefreshResponse, json, login } from './transfer-browser-support.js'

async function verifyResourceCombinationFilters(page, api, env, principal, navigate) {
  const endpoint = '/api/v1/security/protection-enrollments'
  const baseline = await json(await api.get(`${endpoint}?scope=all&page=1&page_size=100`), 'ordinary user resource baseline')
  expect(baseline.total).toBe(baseline.data.length)
  expect(baseline.data.length).toBeGreaterThan(1)
  expect(baseline.data.length).toBeLessThanOrEqual(20)
  const mongoID = Number(env.ADDP_ONLINE_SECURITY_MONGODB_ENGINE_ID)
  const mysqlID = Number(new URL(env.ADDP_ONLINE_SECURITY_EXPORT_LOCATOR).pathname.split('/')[1])
  expect(mongoID).toBeGreaterThan(0)
  expect(mysqlID).not.toBe(mongoID)
  const targets = baseline.data.filter(row => row.target_snapshot.engine_id === mongoID && row.target_snapshot.full_name === 'Outdoor.Persons')
  expect(targets).toHaveLength(1)
  expect(targets[0].state).toBe('active')
  const writes = []
  const observe = request => {
    if (new URL(request.url()).pathname.startsWith('/api/v1/security/') && request.method() !== 'GET') {
      writes.push({ method: request.method(), path: new URL(request.url()).pathname })
    }
  }
  page.on('request', observe)
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  const filters = frame.locator('.resource-filters')
  const name = filters.locator('.el-form-item').filter({ hasText: /资源名称|Resource name/ }).getByRole('textbox')
  const engine = filters.locator('.el-form-item').filter({ hasText: /引擎|Engine/ }).locator('.el-select')
  const state = filters.locator('.el-form-item').filter({ hasText: /保护状态|Protection state/ }).locator('.el-select')
  const apply = filters.getByRole('button', { name: /^(筛选|Filter)$/ })
  const reset = filters.getByRole('button', { name: /^(重置|Reset)$/ })
  const rows = frame.locator('.enrollment-card .el-table__body tbody tr')
  const cases = new Set()
  async function checkCase(label, query, action) {
    const expectedQuery = { scope: 'current', page: '1', page_size: '20', ...query }
    const response = page.waitForResponse(response => {
      const url = new URL(response.url())
      return url.pathname === endpoint && response.request().method() === 'GET' &&
        JSON.stringify(Object.fromEntries([...url.searchParams].sort())) === JSON.stringify(Object.fromEntries(Object.entries(expectedQuery).sort()))
    })
    await action()
    const payload = await json(await response, `ordinary user filter ${label}`)
    const expected = baseline.data.filter(row =>
      (expectedQuery.scope === 'all' || (expectedQuery.scope === 'released') === (row.state === 'released')) &&
      (!query.resource_name || row.target_snapshot.full_name.toLowerCase().includes(query.resource_name.toLowerCase())) &&
      (!query.engine_id || String(row.target_snapshot.engine_id) === query.engine_id) &&
      (!query.state || row.state === query.state))
    expect(payload.total).toBe(expected.length)
    expect(payload.page).toBe(1)
    expect(payload.page_size).toBe(20)
    expect(payload.data.map(row => row.id).sort()).toEqual(expected.map(row => row.id).sort())
    await expect(rows).toHaveCount(expected.length)
    expect((await rows.locator('.resource-path').allTextContents()).sort()).toEqual(expected.map(row => row.target_snapshot.full_name).sort())
    await expect(apply).toBeEnabled()
    if (label) cases.add(label)
    return payload
  }
  async function select(control, label) {
    await control.click()
    await frame.getByRole('option', { name: label, exact: true }).click()
  }
  try {
    await checkCase('', {}, async () => { api = await navigate('/security/protection-enrollments', endpoint) })
    await name.fill('Persons')
    await checkCase('name', { resource_name: 'Persons' }, () => apply.click())
    const engineDefinitions = await json(await api.get('/api/v1/meta/engines'), 'ordinary user catalog engines')
    const mongo = engineDefinitions.find(item => Number(item.id) === mongoID)
    const mysql = engineDefinitions.find(item => Number(item.id) === mysqlID)
    expect(mongo?.name).toBeTruthy()
    expect(mysql?.name).toBeTruthy()
    await select(engine, mongo.name)
    await select(state, /^(保护规则同步完成|Protection rules synchronized)$/)
    const combined = { resource_name: 'Persons', engine_id: String(mongoID), state: 'active' }
    const positive = await checkCase('combined', combined, () => apply.click())
    expect(positive.total).toBe(1)
    const progress = positive.data[0].owner_progress
    expect(progress.map(owner => owner.consumer_owner).sort()).toEqual(['develop', 'manager', 'service', 'transfer'])
    for (const owner of progress) {
      expect(owner.acknowledged).toBe(true)
      expect(owner.validity).toBe('valid')
      expect(positive.data[0].validity_mode).toBe('permanent')
      expect(positive.data[0].expires_at).toBeNull()
      expect(owner.expires_at).toBeNull()
    }
    await expect(rows.locator('.owner-item')).toHaveCount(4)
    await expect(rows.locator('.owner-tags').filter({ hasText: /有效期内|Within validity period/ })).toHaveCount(4)
    const securityFrame = page.frames().find(value => new URL(value.url()).pathname.startsWith('/module-ui/security/'))
    expect(securityFrame).toBeTruthy()
    const dates = await securityFrame.evaluate(values => values.map(value => value === null ? null : new Date(value).toLocaleString()), progress.map(owner => owner.expires_at))
    const visibleDates = await rows.locator('.owner-name small').allTextContents()
    for (const date of dates) expect(visibleDates.some(value => date === null ? /长期有效|Permanent/.test(value) : value.includes(date))).toBe(true)
    await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'security-resource-combination-filters.png') })

    await select(engine, mysql.name)
    expect((await checkCase('wrong-engine', { ...combined, engine_id: String(mysqlID) }, () => apply.click())).total).toBe(0)
    await select(engine, mongo.name)
    await select(state, /^(已停止保护|Protection stopped)$/)
    const stopped = { ...combined, state: 'released' }
    expect((await checkCase('current-released', stopped, () => apply.click())).total).toBe(0)
    await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'security-resource-empty-intersection.png') })
    expect((await checkCase('all-released', { ...stopped, scope: 'all' }, () => frame.locator('.list-scope-bar').getByText(/^(全部|All)$/).click())).total).toBe(0)
    await select(state, /^(保护规则同步完成|Protection rules synchronized)$/)
    expect((await checkCase('all-active', { ...combined, scope: 'all' }, () => apply.click())).total).toBe(1)
    await name.fill('%')
    expect((await checkCase('literal-percent', { ...combined, scope: 'all', resource_name: '%' }, () => apply.click())).total).toBe(0)
    await checkCase('reset-all', { scope: 'all' }, () => reset.click())
    await expect(name).toHaveValue('')
    await expect(engine.locator('.el-select__placeholder.is-transparent')).toBeVisible()
    await expect(state.locator('.el-select__placeholder.is-transparent')).toBeVisible()
    await checkCase('current-restored', {}, () => frame.locator('.list-scope-bar').getByText(/^(保护中|Protected)$/).click())
    await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'security-resource-filter-reset.png') })
    const current = identity(await json(await api.get('/api/v1/system/auth/context'), 'ordinary user after filters'))
    expect(current.principalID).toBe(principal.principalID)
    expect(current.principalType).toBe('user')
    expect(current.contextType).toBe('tenant')
    expect(current.tenantID).toBe(principal.tenantID)
    expect([...current.permissions].sort()).toEqual([...principal.permissions].sort())
    expect(writes).toEqual([])
    expect(cases.size).toBe(9)
    return { resource_combination_filters_verified: true, resource_literal_filter_verified: true,
      resource_scope_intersection_verified: true, resource_reset_verified: true,
      resource_filter_total_verified: true, resource_filters_read_only: true,
      owner_validity_visible: true, resource_filter_case_count: cases.size }
  } finally {
    page.off('request', observe)
  }
}

async function verifyDevelopProtectionRefusal(page, api, env, navigate) {
  const locator = env.ADDP_ONLINE_SECURITY_EXPORT_LOCATOR
  const engineID = Number(new URL(locator).pathname.split('/')[1])
  async function execute(query) {
    const started = await json(await api.post('/api/v1/develop/executions', { data: {
      dev_type: 'query', trigger_type: 'manual',
      content: { query, query_type: 'sql', target_locator: locator, query_parameters: [] },
      execution_config: { engine_id: engineID }, parameters: {}, timeout: 120
    } }), 'ordinary user Develop query')
    expect(started.execution_id).toMatch(/^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/)
    let execution
    await expect.poll(async () => {
      execution = await json(await api.get(`/api/v1/develop/executions/${started.execution_id}`), 'Develop query outcome')
      return execution.status
    }, { timeout: 45_000 }).toMatch(/^(success|failed|timeout|cancelled)$/)
    expect(execution.execution_id).toBe(started.execution_id)
    expect(execution.source_task_id == null).toBe(true)
    return execution
  }
  const allowed = await execute('SELECT id, customer_code, email AS contact_email FROM customers ORDER BY id')
  expect(allowed.status).toBe('success')
  const rows = allowed.metadata.result.summary.preview_rows
  expect(rows).toHaveLength(5)
  for (const row of rows) {
    expect(row).not.toHaveProperty('email')
    expect(row).not.toHaveProperty('contact_email')
    expect(row.customer_code).toBe(`CUST-1000${row.id}`)
  }
  const denied = await execute("SELECT CONCAT(email, '') AS contact_email FROM customers ORDER BY id")
  expect(denied.status).toBe('failed')
  expect(denied.metadata || {}).not.toHaveProperty('result')
  expect(Object.keys(denied.error_details).sort()).toEqual(['category', 'code', 'diagnostic'])
  expect(denied.error_details.code).toBe('develop.query.protection_required')
  expect(denied.error_details.diagnostic).toEqual({ schema_version: 'addp.execution-failure/v2',
    stage: 'preparation', reason: 'data_protection_required' })
  const executionPath = `/api/v1/develop/executions/${denied.execution_id}`
  const currentAPI = await navigate(`/develop/executions/${denied.execution_id}`, executionPath)
  const rebound = await json(await currentAPI.get(executionPath), 'same-user protection refusal')
  expect(rebound.error_details).toEqual(denied.error_details)
  expect(rebound.metadata || {}).not.toHaveProperty('result')
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  async function checkPanel(panel) {
    await expect(panel).toBeVisible()
    await expect(panel).toContainText(/本次查询已被阻止|The query was blocked/)
    await expect(panel).toContainText(/请直接选择受保护字段或使用字段别名|Select protected fields directly or with column aliases/)
    await expect(panel).toContainText('develop.query.protection_required')
    await expect(panel).toContainText(denied.execution_id)
  }
  const ownerPanel = frame.locator('.execution-failure')
  await checkPanel(ownerPanel)
  await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'security-develop-protection-refusal.png') })
  const monitorLink = ownerPanel.getByRole('link', { name: /查看监控错误详情|View error details in Monitor/ })
  const href = await monitorLink.getAttribute('href')
  const target = new URL(href, page.url())
  expect(target.origin).toBe(new URL(page.url()).origin)
  expect(target.pathname).toBe('/monitor/executions')
  expect([...target.searchParams]).toEqual([['execution_id', denied.execution_id]])
  await navigate(target.pathname + target.search, `/api/v1/monitor/executions/by-execution-id/${denied.execution_id}/tree`)
  await checkPanel(frame.locator('.execution-detail-content > .execution-failure'))
  await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'security-develop-protection-monitor.png') })
  return { develop_alias_protected: true, develop_refusal_rows: 0,
    develop_refusal_diagnostic_verified: true, develop_refusal_visible: true, develop_monitor_same_execution: true }
}


test('ordinary-user queries and Manager export preserve protection diagnostics and provenance in Monitor', async ({ page }) => {
  const names = [
    'ADDP_ONLINE_ARTIFACT_DIR', 'ADDP_ONLINE_TEST_RUN_ID', 'ADDP_ONLINE_TEST_TENANT_ID',
    'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN', 'ADDP_ONLINE_TEST_USER_USERNAME',
    'ADDP_ONLINE_TEST_USER_PASSWORD', 'ADDP_ONLINE_SECURITY_EXPORT_LOCATOR',
    'ADDP_ONLINE_SECURITY_MONGODB_ENGINE_ID', 'GATEWAY_URL'
  ]
  for (const name of names) if (!process.env[name]) throw new Error(`${name} is required`)
  const env = process.env
  const api = await request.newContext({
    baseURL: env.GATEWAY_URL,
    extraHTTPHeaders: { Authorization: `Bearer ${env.ADDP_ONLINE_TEST_USER_ACCESS_TOKEN}` }
  })
  const browserErrors = []
  const failedResponses = []
  let businessStarted = false
  let anonymousRefresh401 = 0
  let browserAPI
  let download
  page.on('pageerror', error => browserErrors.push(error.message))
  page.on('console', message => {
    if (isAnonymousRefreshConsoleError(message, businessStarted)) return
    if (['warning', 'error'].includes(message.type())) browserErrors.push(message.text())
  })
  page.on('response', response => {
    const path = new URL(response.url()).pathname
    if (isAnonymousRefreshResponse(response, businessStarted)) anonymousRefresh401 += 1
    if (businessStarted && path.startsWith('/api/v1/') && response.status() >= 400) {
      failedResponses.push({ path, status: response.status() })
    }
  })
  try {
    const requiredPermissions = [
      'manager.content.read', 'manager.data_item.read', 'manager.search.execute', 'manager.derived_artifact.create',
      'manager.derived_artifact.read', 'monitor.execution.read', 'security.enrollment.read', 'meta.catalog.read', 'develop.task.read', 'develop.task.execute', 'develop.data_read.execute'
    ]
    const expected = identity(await json(await api.get('/api/v1/system/auth/context'), 'API AuthContext'))
    expect(expected.principalType).toBe('user')
    expect(expected.contextType).toBe('tenant')
    expect(expected.permissions.has('*')).toBe(false)
    expect(expected.tenantID).toBe(env.ADDP_ONLINE_TEST_TENANT_ID)
    for (const permission of requiredPermissions) expect(expected.permissions.has(permission), permission).toBe(true)
    const path = `/manager/data-explorer?locator=${encodeURIComponent(env.ADDP_ONLINE_SECURITY_EXPORT_LOCATOR)}`
    const previewResponse = page.waitForResponse(response =>
      new URL(response.url()).pathname === '/api/v1/manager/preview' && response.request().method() === 'GET'
    )
    const token = await login(page, env.ADDP_ONLINE_TEST_USER_USERNAME, env.ADDP_ONLINE_TEST_USER_PASSWORD, path)
    businessStarted = true
    browserAPI = await request.newContext({
      baseURL: env.GATEWAY_URL,
      extraHTTPHeaders: { Authorization: `Bearer ${token}` }
    })
    const actual = identity(await json(await browserAPI.get('/api/v1/system/auth/context'), 'browser AuthContext'))
    expect(actual.principalID).toBe(expected.principalID)
    expect(actual.principalType).toBe('user')
    expect(actual.contextType).toBe('tenant')
    expect(actual.tenantID).toBe(expected.tenantID)
    for (const permission of requiredPermissions) {
      expect(actual.permissions.has(permission)).toBe(true)
    }
    let browserSessionRebinds = 0
    async function navigateWithBrowserSession(path, endpoint) {
      const moduleUI = `/module-ui/${new URL(path, page.url()).pathname.split('/')[1]}/`
      const response = page.waitForResponse(response =>
        new URL(response.url()).pathname === endpoint && response.request().method() === 'GET' &&
        new URL(response.request().frame().url()).pathname.startsWith(moduleUI))
      await page.goto(path)
      const currentAPI = await browserAPIFromResponse(await response, env.GATEWAY_URL, actual)
      await browserAPI.dispose()
      browserAPI = currentAPI
      browserSessionRebinds += 1
      return currentAPI
    }

    const preview = await json(await previewResponse, 'protected preview from browser')
    expect(preview.preview_type).toBe('table')
    expect(preview.data.columns).toContain('customer_code')
    expect(preview.data.columns).not.toContain('email')
    expect(preview.data.rows).toHaveLength(5)
    for (const row of preview.data.rows) {
      expect(row).not.toHaveProperty('email')
      expect(row.customer_code).toBeTruthy()
    }
    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(frame.getByRole('columnheader', { name: 'customer_code', exact: true })).toBeVisible()
    await expect(frame.getByRole('columnheader', { name: 'email', exact: true })).toHaveCount(0)
    await expect(frame.locator('.table-preview .el-table__body tbody tr')).toHaveCount(5)
    await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'security-manager-protected-preview.png') })

    const resourceFilters = await verifyResourceCombinationFilters(page, browserAPI, env, actual, navigateWithBrowserSession)
    const developProtection = await verifyDevelopProtectionRefusal(page, browserAPI, env, navigateWithBrowserSession)
    await navigateWithBrowserSession(path, '/api/v1/manager/preview')
    await expect(frame.getByRole('columnheader', { name: 'customer_code', exact: true })).toBeVisible()

    await page.locator('.sidebar').getByText(/^(数据检索|Data Retrieval)$/).click()
    await expect(page).toHaveURL(/\/manager\/data-retrieval$/)
    await frame.locator('.search-input input').fill('email')
    const searchResponse = page.waitForResponse(response =>
      new URL(response.url()).pathname === '/api/v1/manager/search' && response.request().method() === 'GET'
    )
    await frame.getByRole('button', { name: /^(搜索|Search)$/ }).click()
    const search = await json(await searchResponse, 'protected field search from browser')
    const expectedLocator = new URL(env.ADDP_ONLINE_SECURITY_EXPORT_LOCATOR)
    expectedLocator.searchParams.sort()
    const hits = search.data.results.filter(hit => {
      if (!hit.locator) return false
      const locator = new URL(hit.locator)
      locator.searchParams.sort()
      return locator.href === expectedLocator.href
    })
    expect(hits).toHaveLength(1)
    const hit = hits[0]
    expect(hit.engine_id).toBe(Number(expectedLocator.pathname.split('/')[1]))
    const email = hit.field_matches.filter(field => field.name === 'email')
    expect(email).toHaveLength(1)
    expect(email[0].data_type).toBe('string')
    expect(email[0].highlights.name).toBe('<mark>email</mark>')
    const result = frame.locator('.result-item').filter({ hasText: hit.full_name })
    await expect(result).toHaveCount(1)
    const field = result.locator('.field-match').filter({ has: frame.locator('.field-name', { hasText: /^email$/ }) })
    await expect(field.locator('.field-name mark')).toHaveText('email')
    await expect(field.locator('.field-type')).toHaveText('string')
    await result.scrollIntoViewIfNeeded()
    await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'security-manager-field-search.png') })
    await result.getByRole('button', { name: /^(定位|Locate)$/ }).click()
    await expect.poll(() => {
      const current = new URL(page.url())
      const locator = current.searchParams.get('locator')
      if (!locator || current.pathname !== '/manager/data-explorer') return false
      const actual = new URL(locator)
      actual.searchParams.sort()
      return actual.href === expectedLocator.href
    }).toBe(true)
    await expect(frame.getByRole('columnheader', { name: 'customer_code', exact: true })).toBeVisible()
    await expect(frame.getByRole('columnheader', { name: 'email', exact: true })).toHaveCount(0)

    await frame.getByRole('button', { name: /导出全部结果|Export All Results/i }).click()
    const dialog = frame.getByRole('dialog', { name: /导出全部结果|Export All Results/i })
    await expect(dialog).toBeVisible()
    await dialog.locator('.el-select__wrapper').click()
    await frame.getByRole('option', { name: 'CSV', exact: true }).click()
    const fileName = `security-export-${env.ADDP_ONLINE_TEST_RUN_ID}`
    await dialog.locator('.el-input input').last().fill(fileName)
    const createdResponse = page.waitForResponse(response =>
      new URL(response.url()).pathname === '/api/v1/manager/exports' && response.request().method() === 'POST'
    )
    const downloaded = page.waitForEvent('download', { timeout: 120_000 })
    await dialog.getByRole('button', { name: /开始导出|Start Export/ }).click()
    const response = await createdResponse
    expect(response.status()).toBe(202)
    expect(response.request().postDataJSON()).toEqual({
      source_item_locator: env.ADDP_ONLINE_SECURITY_EXPORT_LOCATOR, format: 'csv', file_name: fileName
    })
    const session = await json(response, 'create export from browser')
    expect(session.transfer_execution_id).toMatch(/^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/)
    download = await downloaded
    expect(await download.failure()).toBeNull()
    const stream = await download.createReadStream()
    const chunks = []
    for await (const chunk of stream) chunks.push(chunk)
    const rows = JSON.parse(execFileSync('python3', ['-c',
      'import csv, io, json, sys; print(json.dumps(list(csv.DictReader(io.StringIO(sys.stdin.buffer.read().decode("utf-8-sig"))))))'
    ], { input: Buffer.concat(chunks), encoding: 'utf8' }))
    expect(rows).toHaveLength(5)
    expect(rows.map(row => String(row.id)).sort()).toEqual(['1', '2', '3', '4', '5'])
    for (const row of rows) {
      expect(row).not.toHaveProperty('email')
      expect(row.customer_code).toBeTruthy()
    }
    const ready = await json(await browserAPI.get(`/api/v1/manager/exports/${session.id}`), 'completed export')
    expect(ready.status).toBe('success')
    expect(ready.transfer_execution_id).toBe(session.transfer_execution_id)
    expect(ready.source_item_locator).toBe(env.ADDP_ONLINE_SECURITY_EXPORT_LOCATOR)
    await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'security-manager-export.png') })

    const executionPath = `/api/v1/monitor/executions/by-execution-id/${session.transfer_execution_id}`
    const execution = await json(await browserAPI.get(executionPath), 'Monitor export execution')
    expect(execution.execution_id).toBe(session.transfer_execution_id)
    expect(execution.module).toBe('transfer')
    expect(execution.source).toBe('manager')
    expect(execution.status).toBe('success')
    expect(execution.source_task_id == null).toBe(true)
    expect(String(execution.triggered_by)).toBe(actual.principalID)
    expect(Number(execution.records_read)).toBe(5)
    expect(Number(execution.records_written)).toBe(5)
    await page.goto(`/monitor/executions?execution_id=${session.transfer_execution_id}`)
    const detail = page.frameLocator('iframe[data-testid="module-iframe"]').locator('.execution-detail-content')
    await expect(detail).toBeVisible()
    await expect(detail).toContainText(session.transfer_execution_id)
    await expect(detail.getByText('transfer', { exact: true }).first()).toBeVisible()
    await expect(detail.getByText('manager', { exact: true }).first()).toBeVisible()
    await expect(detail.locator('.el-tag').filter({ hasText: /^(成功|Success)$/ }).first()).toBeVisible()
    await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'security-manager-export-monitor.png') })
    expect(failedResponses).toEqual([])
    expect(browserErrors).toEqual([])
    expect(anonymousRefresh401).toBe(1)
    expect(browserSessionRebinds).toBe(4)
    writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'security-manager-export-browser.json'), `${JSON.stringify({
      schema_version: 'addp.security-manager-export-browser/v6', result: 'passed',
      run_id: env.ADDP_ONLINE_TEST_RUN_ID, tenant_id: actual.tenantID,
      execution_id: session.transfer_execution_id, records: rows.length,
      email_field_present: false, non_sensitive_fields_preserved: true,
      same_user_verified: true, initiator_verified: true, taskless_execution: true,
      manager_source_verified: true, monitor_detail_visible: true,
      protected_preview_verified: true, technical_field_search_verified: true,
      search_to_preview_verified: true,
      ...resourceFilters,
      ...developProtection,
      browser_session_rebound_verified: browserSessionRebinds === 4, resource_permanent_validity_visible: true,
      browser_warning_errors: 0, failed_business_responses: 0, anonymous_refresh_401: anonymousRefresh401
    })}\n`, 'utf8')
  } catch (error) {
    const current = new URL(page.url())
    console.error('Security export route at failure:', JSON.stringify({
      pathname: current.pathname, query_keys: [...current.searchParams.keys()],
      locator_matches_expected: current.searchParams.get('locator') === env.ADDP_ONLINE_SECURITY_EXPORT_LOCATOR
    }))
    throw error
  } finally {
    if (download) await download.delete()
    if (browserAPI) await browserAPI.dispose()
    await api.dispose()
  }
})
