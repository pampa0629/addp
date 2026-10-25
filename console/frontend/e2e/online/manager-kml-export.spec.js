import { expect, test } from '@playwright/test'
import { createHash } from 'node:crypto'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { createBrowserGpuDiagnostics } from '../../../../common-frontend/basic/src/utils/browserGpuDiagnostics.mjs'
import { browserAPIFromResponse, isAnonymousRefreshConsoleError, isAnonymousRefreshResponse, json, login } from './transfer-browser-support.js'

test('ordinary user exports WGS84 and projected PostGIS tables as KML through real buttons', async ({ page }) => {
  test.setTimeout(300_000)
  const env = process.env
  const context = JSON.parse(env.ADDP_ONLINE_MANAGER_KML_JSON || '{}')
  expect(context.run_id).toBe(env.ADDP_ONLINE_TEST_RUN_ID)
  expect(context.cases.map(source => source.source_srid)).toEqual([4326, 3857])
  const expected = { principalID: context.identity.principal_id, tenantID: context.identity.tenant_id,
    permissions: new Set(context.identity.permissions_verified) }
  expect(expected.permissions.size).toBe(8)
  expect(expected.permissions.has('transfer.task.read')).toBe(true)
  const diagnostics = createBrowserGpuDiagnostics('kml-export')
  const warnings = []
  const failedResponses = []
  const cases = []
  let passed = false
  let businessStarted = false
  page.on('request', event => {
    if (new URL(event.url()).pathname === '/api/v1/system/auth/context' && event.headers().authorization) businessStarted = true
  })
  page.on('console', message => {
    if (isAnonymousRefreshConsoleError(message, businessStarted) || diagnostics.record(message)) return
    if (['warning', 'error'].includes(message.type())) warnings.push({ type: message.type(), message: message.text() })
  })
  page.on('pageerror', error => warnings.push({ type: 'pageerror', message: error.message }))
  page.on('response', response => {
    if (isAnonymousRefreshResponse(response, businessStarted)) return
    const path = new URL(response.url()).pathname
    if (path.startsWith('/api/v1/') && response.status() >= 400) failedResponses.push({ path, status: response.status() })
  })
  try {
    await login(page, env.ADDP_ONLINE_TEST_USER_USERNAME, env.ADDP_ONLINE_TEST_USER_PASSWORD, '/')
    for (const source of context.cases) {
      const outcome = { source_case: source.source_case, locator: source.locator,
        actual_button_download: false, monitor_verified: false }
      cases.push(outcome)
      const observed = page.waitForResponse(response => {
        const url = new URL(response.url())
        return url.pathname === '/api/v1/manager/resource-actions' && url.searchParams.get('locator') === source.locator
      })
      observed.catch(() => {})
      await page.goto('/manager/data-explorer?locator=' + encodeURIComponent(source.locator))
      const api = await browserAPIFromResponse(await observed, env.GATEWAY_URL, expected)
      try {
        const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
        await expect(frame.getByRole('button', { name: /^(导出全部结果|Export All Results)$/ })).toBeVisible()
        await expect(frame.getByRole('columnheader', { name: 'seq', exact: true })).toBeVisible()
        await frame.getByRole('button', { name: /^(导出全部结果|Export All Results)$/ }).click()
        const dialog = frame.getByRole('dialog', { name: /^(导出全部结果|Export All Results)$/ })
        await expect(dialog).toBeVisible()
        await dialog.locator('.el-select__wrapper').click()
        await frame.getByRole('option', { name: 'KML', exact: true }).click()
        await dialog.locator('.el-input input').last().fill(source.filename)
        await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, source.dialog_screenshot) })
        const created = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/manager/exports'
          && response.request().method() === 'POST')
        created.catch(() => {})
        const downloading = page.waitForEvent('download', { timeout: 120_000 })
        downloading.catch(() => {})
        await dialog.getByRole('button', { name: /^(开始导出|Start Export)$/ }).click()
        const response = await created
        expect(response.status()).toBe(202)
        expect(response.request().postDataJSON()).toEqual({ source_item_locator: source.locator, format: 'kml', file_name: source.filename })
        const session = await json(response, 'KML export session')
        Object.assign(outcome, { session_id: session.id, execution_id: session.transfer_execution_id })
        const download = await downloading
        try {
          expect(await download.failure()).toBeNull()
          const stream = await download.createReadStream()
          const chunks = []
          for await (const chunk of stream) chunks.push(chunk)
          const raw = Buffer.concat(chunks)
          writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, source.download_file), raw)
          expect(download.suggestedFilename()).toBe(source.filename + '.kml')
          const ready = await json(await api.get(`/api/v1/manager/exports/${session.id}`), 'KML ready session')
          expect(ready.status).toBe('success')
          expect(ready.format).toBe('kml')
          expect(ready.transfer_execution_id).toBe(session.transfer_execution_id)
          expect(ready.source_item_locator).toBe(source.locator)
          expect(ready.download_url).toBe(`/api/v1/manager/exports/${session.id}/file`)
          // The second read checks the same session's authenticated file contract;
          // it does not replace the actual button/download proof above.
          const file = await api.get(ready.download_url)
          expect(file.status()).toBe(200)
          expect(file.headers()['content-type']).toContain('application/vnd.google-earth.kml+xml')
          expect(await file.body()).toEqual(raw)
          expect(file.headers()['content-disposition']).toContain('.kml')
          const execution = await json(await api.get('/api/v1/monitor/executions/by-execution-id/' + session.transfer_execution_id), 'KML Monitor execution')
          expect(execution).toMatchObject({ execution_id: session.transfer_execution_id, module: 'transfer', source: 'manager', status: 'success' })
          expect(execution.source_task_id == null).toBe(true)
          expect(String(execution.triggered_by)).toBe(expected.principalID)
          expect(Number(execution.records_read)).toBe(8)
          expect(Number(execution.records_written)).toBe(8)
          await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, source.screenshot) })
          Object.assign(outcome, { source_case: source.source_case, locator: source.locator, session_id: session.id,
            execution_id: session.transfer_execution_id, suggested_filename: download.suggestedFilename(),
            actual_button_download: true, monitor_verified: true, size_bytes: raw.length,
            sha256: createHash('sha256').update(raw).digest('hex') })
        } finally {
          await download.delete()
        }
      } finally {
        await api.dispose()
      }
    }
    expect(warnings).toEqual([])
    expect(failedResponses).toEqual([])
    passed = true
  } finally {
    writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'manager-kml-export-browser.json'), JSON.stringify({
      schema_version: 'addp.manager-kml-export-browser/v1', result: passed ? 'passed' : 'failed', run_id: context.run_id,
      identity: context.identity, cases, warnings, failed_responses: failedResponses,
      gpu_diagnostics: diagnostics.snapshot()
    }, null, 2) + '\n')
  }
})
