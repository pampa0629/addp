import { expect, request, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { identity, json, login } from './transfer-browser-support.js'

test('protected Manager export retains its verified initiator and opens in Monitor', async ({ page }) => {
  const names = [
    'ADDP_ONLINE_ARTIFACT_DIR', 'ADDP_ONLINE_TEST_RUN_ID', 'ADDP_ONLINE_TEST_TENANT_ID',
    'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN', 'ADDP_ONLINE_TEST_USER_USERNAME',
    'ADDP_ONLINE_TEST_USER_PASSWORD', 'ADDP_ONLINE_SECURITY_EXPORT_LOCATOR', 'GATEWAY_URL'
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
  let browserAPI
  let download
  page.on('pageerror', error => browserErrors.push(error.message))
  page.on('console', message => {
    if (['warning', 'error'].includes(message.type())) browserErrors.push(message.text())
  })
  page.on('response', response => {
    const path = new URL(response.url()).pathname
    if (businessStarted && path.startsWith('/api/v1/') && response.status() >= 400) {
      failedResponses.push({ path, status: response.status() })
    }
  })
  try {
    const requiredPermissions = [
      'manager.content.read', 'manager.data_item.read', 'manager.derived_artifact.create',
      'manager.derived_artifact.read', 'monitor.execution.read'
    ]
    const expected = identity(await json(await api.get('/api/v1/system/auth/context'), 'API AuthContext'))
    expect(expected.principalType).toBe('user')
    expect(expected.contextType).toBe('tenant')
    expect(expected.tenantID).toBe(env.ADDP_ONLINE_TEST_TENANT_ID)
    for (const permission of requiredPermissions) expect(expected.permissions.has(permission), permission).toBe(true)
    const path = `/manager/data-explorer?locator=${encodeURIComponent(env.ADDP_ONLINE_SECURITY_EXPORT_LOCATOR)}`
    const token = await login(page, env.ADDP_ONLINE_TEST_USER_USERNAME, env.ADDP_ONLINE_TEST_USER_PASSWORD, path)
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
    businessStarted = true

    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    await frame.getByRole('button', { name: /导出全部结果|Export All Results/i }).click()
    const dialog = frame.getByRole('dialog', { name: /导出全部结果|Export All Results/i })
    await expect(dialog).toBeVisible()
    await dialog.locator('.el-select__wrapper').click()
    await frame.getByRole('option', { name: 'JSONL', exact: true }).click()
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
      source_item_locator: env.ADDP_ONLINE_SECURITY_EXPORT_LOCATOR, format: 'jsonl', file_name: fileName
    })
    const session = await json(response, 'create export from browser')
    expect(session.transfer_execution_id).toMatch(/^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/)
    download = await downloaded
    expect(await download.failure()).toBeNull()
    const stream = await download.createReadStream()
    const chunks = []
    for await (const chunk of stream) chunks.push(chunk)
    const rows = Buffer.concat(chunks).toString('utf8').trim().split('\n').map(line => JSON.parse(line))
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
    writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'security-manager-export-browser.json'), `${JSON.stringify({
      schema_version: 'addp.security-manager-export-browser/v1', result: 'passed',
      run_id: env.ADDP_ONLINE_TEST_RUN_ID, tenant_id: actual.tenantID,
      execution_id: session.transfer_execution_id, records: rows.length,
      email_field_present: false, non_sensitive_fields_preserved: true,
      same_user_verified: true, initiator_verified: true, taskless_execution: true,
      manager_source_verified: true, monitor_detail_visible: true,
      browser_warning_errors: 0, failed_business_responses: 0
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
