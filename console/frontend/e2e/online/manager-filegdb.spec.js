import { expect, test } from '@playwright/test'
import { createHash } from 'node:crypto'
import { readFileSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { browserAPIFromResponse, isAnonymousRefreshConsoleError, isAnonymousRefreshResponse, login } from './transfer-browser-support.js'

test('ordinary User saves or is denied the same FileGDB through the real download button', async ({ page }) => {
  const env = process.env
  const input = JSON.parse(env.ADDP_ONLINE_MANAGER_FILEGDB_JSON || '{}')
  expect(input.run_id).toBe(env.ADDP_ONLINE_TEST_RUN_ID)
  expect(['ungranted', 'granted', 'revoked', 'expired']).toContain(input.phase)
  expect(input.status).toBe(input.phase === 'granted' ? 200 : 403)
  const expected = { principalID: input.identity.principal_id, tenantID: input.identity.tenant_id,
    permissions: new Set(input.identity.permissions_verified) }
  expect([...expected.permissions].sort()).toEqual(['manager.content.read', 'manager.data_item.read',
    'meta.catalog.read', 'meta.scan_task.execute', 'meta.scan_task.read', 'monitor.execution.read'])
  const engine = /^addp:\/\/engine\/([1-9][0-9]*)\/path\/authorization\.gdb\?type=file&item_id=[1-9][0-9]*$/.exec(input.locator)?.[1]
  expect(engine).toBeTruthy()
  let businessStarted = false, anonymousRefresh = 0, downloadEvents = 0
  const warnings = [], pageErrors = [], unexpected = [], writes = [], previewDenials = []
  const proof = { schema_version: 'addp.manager-filegdb-browser/v1', result: 'failed',
    run_id: input.run_id, phase: input.phase, locator: input.locator, identity: input.identity }
  const exactSource = url => url.searchParams.get('locator') === input.locator
  const sourceDownload = response => {
    const url = new URL(response.url())
    return url.pathname === '/api/v1/manager/downloads/file' && exactSource(url)
  }
  page.on('download', () => downloadEvents++)
  page.on('request', request => {
    const url = new URL(request.url()), path = url.pathname
    if (path === '/api/v1/system/auth/context' && request.headers().authorization) businessStarted = true
    const readOnlyCatalog = request.method() === 'POST' && path === `/api/v1/system/engines/${engine}/catalog/children`
    if (!['GET', 'HEAD'].includes(request.method()) && !readOnlyCatalog &&
        /^\/api\/v1\/(manager|meta|system\/engines)(\/|$)/.test(path)) writes.push({ path, method: request.method() })
  })
  page.on('response', response => {
    if (isAnonymousRefreshResponse(response, businessStarted)) { anonymousRefresh++; return }
    const url = new URL(response.url())
    if (!url.pathname.startsWith('/api/v1/') || response.status() < 400) return
    if (input.status === 403 && response.status() === 403) {
      if (sourceDownload(response)) return
      if (url.pathname === '/api/v1/manager/preview' && exactSource(url) &&
          ['initial_points', 'added_points'].includes(url.searchParams.get('child_name'))) {
        previewDenials.push({ child: url.searchParams.get('child_name'), status: 403 }); return
      }
    }
    unexpected.push({ path: url.pathname, status: response.status() })
  })
  page.on('console', message => {
    if (isAnonymousRefreshConsoleError(message, businessStarted)) return
    if (!['warning', 'error'].includes(message.type())) return
    const url = new URL(message.location().url || 'http://addp.invalid')
    if (input.status === 403 && message.type() === 'error') {
      const network403 = message.text() === 'Failed to load resource: the server responded with a status of 403 (Forbidden)'
      if (network403 && exactSource(url) && ['/api/v1/manager/preview', '/api/v1/manager/downloads/file'].includes(url.pathname)) return
      // The real container UI reports its automatic child read's explicit denial.
      if (previewDenials.length && /^(加载预览失败:|加载节点数据失败:)/.test(message.text())) return
    }
    warnings.push({ type: message.type(), source: url.pathname,
      message: message.text().slice(0, 500).replace(/Bearer\s+\S+/gi, '[redacted]') })
  })
  page.on('pageerror', error => pageErrors.push(error.message))
  try {
    await login(page, env.ADDP_ONLINE_TEST_USER_USERNAME, env.ADDP_ONLINE_TEST_USER_PASSWORD, '/')
    const observed = page.waitForResponse(response => {
      const url = new URL(response.url())
      return url.pathname === '/api/v1/manager/resource-actions' && exactSource(url)
    })
    observed.catch(() => {})
    await page.goto('/manager/data-explorer?locator=' + encodeURIComponent(input.locator))
    const capability = await observed
    const api = await browserAPIFromResponse(capability, env.GATEWAY_URL, expected)
    try {
      expect((await capability.json()).actions.download).toEqual({ supported: true, requires_current_user: true })
      const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
      const button = frame.getByRole('button', { name: /^(下载|Download)$/ })
      await expect(button).toBeEnabled()
      const responsePromise = page.waitForResponse(sourceDownload)
      responsePromise.catch(() => {})
      const downloadPromise = input.status === 200 ? page.waitForEvent('download') : null
      downloadPromise?.catch(() => {})
      await button.click()
      proof.actual_button_clicked = true
      const response = await responsePromise, request = response.request(), url = new URL(response.url())
      expect(response.status()).toBe(input.status)
      expect(request.method()).toBe('GET')
      expect([...url.searchParams.keys()]).toEqual(['locator'])
      expect(request.headers().authorization).toBe(capability.request().headers().authorization)
      expect(request.headers().authorization).toMatch(/^Bearer addp_at_\S+$/)
      expect(request.headers().cookie).toBeUndefined()
      Object.assign(proof, { status: response.status(), current_user_bearer: true, cookie_absent: true, credential_in_url: false })
      if (input.status === 200) {
        const download = await downloadPromise
        try {
          expect(await download.failure()).toBeNull()
          expect(download.suggestedFilename()).toBe('authorization.gdb.zip')
          await download.saveAs(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, input.download_file))
          const raw = readFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, input.download_file))
          expect(raw).toEqual(await response.body())
          expect(response.headers()['content-type']).toBe('application/zip')
          expect(response.headers()['content-disposition']).toBe('attachment; filename=authorization.gdb.zip')
          expect(response.headers()['content-length']).toBe(String(raw.length))
          Object.assign(proof, { suggested_filename: download.suggestedFilename(), size: raw.length,
            sha256: createHash('sha256').update(raw).digest('hex') })
        } finally { await download.delete() }
        await expect(frame.getByText(/^(下载已开始|Download started)$/)).toBeVisible()
      } else {
        const error = await response.json()
        expect(Object.keys(error)).toEqual(['error'])
        expect(typeof error.error).toBe('string'); expect(error.error.length).toBeGreaterThan(0)
        await expect(frame.locator('.el-message--error')).toBeVisible()
        await expect(frame.getByText(/^(下载已开始|Download started)$/)).toHaveCount(0)
        Object.assign(proof, { error_visible: true, success_visible: false })
      }
      await expect(button).toBeEnabled()
      expect(downloadEvents).toBe(input.status === 200 ? 1 : 0)
      await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, input.screenshot) })
      proof.screenshot = input.screenshot
    } finally { await api.dispose() }
    expect(writes).toEqual([]); expect(unexpected).toEqual([])
    expect(warnings).toEqual([]); expect(pageErrors).toEqual([])
    expect(anonymousRefresh).toBeLessThanOrEqual(1)
    proof.result = 'passed'
  } finally {
    Object.assign(proof, { download_events: downloadEvents, anonymous_refresh_401: anonymousRefresh,
      warnings, page_errors: pageErrors, unexpected_responses: unexpected, business_writes: writes,
      expected_preview_denials: previewDenials })
    writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'manager-filegdb-browser-' + input.phase + '.json'), JSON.stringify(proof, null, 2) + '\n')
  }
})
