import { expect, request, test } from '@playwright/test'
import { createHash } from 'node:crypto'
import { readFileSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { createBrowserGpuDiagnostics } from '../../../../common-frontend/basic/src/utils/browserGpuDiagnostics.mjs'
import { isAnonymousRefreshConsoleError, isAnonymousRefreshResponse, json, login } from './transfer-browser-support.js'

test.use({ locale: 'zh-CN' })

test('isolated SGM ordinary User generation, task page and cached GLB', async ({ page }) => {
  test.setTimeout(300_000)
  const env = process.env
  expect(env.ADDP_ONLINE_LOCAL_SGM).toBe('1')
  expect(env.GITHUB_ACTIONS).toBeUndefined()
  expect(env.ADDP_ONLINE_HOSTED).toBeUndefined()
  expect(env.ADDP_ONLINE_OWNER_MANAGED).toBeUndefined()
  const source = JSON.parse(env.ADDP_SGM_SOURCE_JSON)
  const api = await request.newContext({ baseURL: env.GATEWAY_URL,
    extraHTTPHeaders: { Authorization: `Bearer ${env.ADDP_ONLINE_TEST_USER_ACCESS_TOKEN}` } })
  let authenticated = false
  let generationRequests = 0
  const errors = []
  const failures = []
  const gpu = createBrowserGpuDiagnostics('login')
  let stage = 'login'
  const mark = value => { stage = value; gpu.setStage(value) }
  page.on('request', req => {
    if (new URL(req.url()).pathname === '/api/v1/manager/quick-view/actions' && req.method() === 'POST') {
      expect(req.postDataJSON().action).toBe('generate_model_3d_glb')
      generationRequests += 1
    }
  })
  page.on('console', message => {
    if (isAnonymousRefreshConsoleError(message, authenticated) || gpu.record(message)) return
    if (['warning', 'error'].includes(message.type())) {
      const text = message.text()
      const category = text.startsWith('[intlify]') ? 'i18n'
        : text.includes('GPU stall due to ReadPixels') ? 'webgl_read_pixels'
          : text.startsWith('THREE.') ? 'three'
            : text.includes('WebGL') ? 'webgl'
              : text.includes('未找到匹配的预览插件') ? 'missing_preview_plugin'
                : text.startsWith('DataExplorer:') ? 'preview_plugin'
                  : text.startsWith('[Vue warn]') ? 'vue' : 'other'
      const location = message.location()
      let path = ''
      try { path = new URL(location.url).pathname } catch {}
      const publicAsset = /^\/(?:module-ui\/(?:manager|console|system)\/)?assets\/[a-zA-Z0-9_.-]+\.js$/.test(path) ? path : undefined
      const missingKey = text.match(/'((?:common|manager|system|console)\.[a-zA-Z0-9_.]+)' key/)
      errors.push({ type: message.type(), category, stage,
        message_sha256: createHash('sha256').update(text).digest('hex'),
        source_asset: publicAsset, source_line: location.lineNumber, source_column: location.columnNumber,
        ...(missingKey ? { i18n_key: missingKey[1] } : {}) })
    }
  })
  page.on('pageerror', () => errors.push('pageerror'))
  page.on('response', response => {
    if (isAnonymousRefreshResponse(response, authenticated)) return
    const path = new URL(response.url()).pathname
    if (path.startsWith('/api/v1/') && response.status() >= 400) {
      const category = ['manager/quick-view', 'manager/preview', 'manager/tasks', 'manager/executions', 'manager', 'meta', 'system', 'monitor']
        .find(prefix => path.startsWith(`/api/v1/${prefix}/`) || path === `/api/v1/${prefix}`) || 'other'
      failures.push({ category, status: response.status() })
    }
  })
  try {
    const route = `/manager/data-explorer?locator=${encodeURIComponent(source.locator)}`
    const token = await login(page, env.ADDP_ONLINE_TEST_USER_USERNAME, env.ADDP_ONLINE_TEST_USER_PASSWORD, route)
    authenticated = true
    const browserAPI = await request.newContext({ baseURL: env.GATEWAY_URL, extraHTTPHeaders: { Authorization: `Bearer ${token}` } })
    const identity = await json(await browserAPI.get('/api/v1/system/auth/context'), 'browser identity')
    const apiIdentity = await json(await api.get('/api/v1/system/auth/context'), 'API identity')
    expect(identity.principal.type).toBe('user')
    expect(identity.principal.id).toBe(apiIdentity.principal.id)
    expect(String(identity.context.tenant_id)).toBe(env.ADDP_ONLINE_TEST_TENANT_ID)
    await browserAPI.dispose()
    mark('generation-entry')
    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const generate = frame.getByRole('button', { name: /生成 GLB 快显|Generate GLB Quick View/ })
    await expect(generate).toBeEnabled()
    mark('original-download')
    const downloadPromise = page.waitForEvent('download')
    await frame.getByRole('button', { name: /下载|Download/, exact: true }).click()
    const download = await downloadPromise
    const bytes = readFileSync(await download.path())
    expect(bytes.length).toBe(source.size_bytes)
    expect(createHash('sha256').update(bytes).digest('hex')).toBe(source.sha256)
    mark('generation')
    const startedPromise = page.waitForResponse(response => response.request().method() === 'POST'
      && new URL(response.url()).pathname === '/api/v1/manager/quick-view/actions')
    await generate.click()
    const startedResponse = await startedPromise
    expect(startedResponse.status()).toBe(202)
    const started = await startedResponse.json()
    expect(started.task_type).toBe('model_3d_glb_generation')
    expect(started.task_id).toBeGreaterThan(0)
    expect(started.execution_id).toMatch(/^[0-9a-f-]{36}$/)
    const query = new URLSearchParams({ locator: source.locator })
    let ready
    await expect.poll(async () => {
      const execution = await json(await api.get(`/api/v1/manager/executions/${started.execution_id}`), 'SGM execution')
      if (['failed', 'timeout', 'cancelled'].includes(execution.status)) throw new Error('SGM execution failed')
      if (execution.status !== 'success') return execution.status
      ready = await json(await api.get(`/api/v1/manager/quick-view/capability?${query}`), 'SGM capability')
      return ready.can_use_quick_view === true && ready.render_source === 'model_3d_glb'
    }, { timeout: 180_000, intervals: [500, 1000, 2000] }).toBe(true)
    const artifact = ready.model_3d
    expect(ready.render_source).toBe('model_3d_glb')
    expect(artifact.task_id).toBe(started.task_id)
    expect(artifact.last_execution_id).toBe(started.execution_id)
    expect(artifact.preview_url).toBe(`/api/v1/manager/model_3d_glb/${artifact.result_id}/content`)
    mark('generated-preview')
    const preview = frame.locator('.model-preview')
    await expect(preview.locator('canvas')).toBeVisible({ timeout: 60_000 })
    await expect(preview.locator('.three-status')).toHaveCount(0, { timeout: 60_000 })
    // Persist the product's existing preference before testing a fresh navigation.
    const preference = await api.patch('/api/v1/manager/preview-state/preferred-mode', {
      data: { locator: source.locator, preferred_mode: 'map_quick_view' }
    })
    expect(preference.status()).toBe(200)
    const content = await api.get(artifact.preview_url)
    expect(content.status()).toBe(200)
    expect((await content.body()).subarray(0, 4).toString()).toBe('glTF')
    const ranged = await api.get(artifact.preview_url, { headers: { Range: 'bytes=0-63' } })
    expect(ranged.status()).toBe(206)
    expect((await ranged.body()).length).toBe(64)
    mark('task-page')
    await page.goto(`/manager/tasks/quick-view?task_type=model_3d_glb_generation&task_id=${started.task_id}`)
    const taskFrame = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(taskFrame.locator('body')).toContainText('compass')
    await expect(taskFrame.getByText(/成功|Succeeded/, { exact: true }).first()).toBeVisible()
    mark('cached-preview')
    const loaded = page.waitForResponse(response => new URL(response.url()).pathname === artifact.preview_url && response.status() === 200)
    await page.goto(route)
    await loaded
    await expect(preview.locator('canvas')).toBeVisible()
    await expect(preview.locator('.three-status')).toHaveCount(0)
    const cached = await json(await api.get(`/api/v1/manager/quick-view/capability?${query}`), 'cached capability')
    expect(cached.model_3d.result_id).toBe(artifact.result_id)
    expect(cached.model_3d.last_execution_id).toBe(started.execution_id)
    expect(generationRequests).toBe(1)
    expect(errors).toEqual([])
    expect(failures).toEqual([])
    writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'sgm-browser.json'), JSON.stringify({
      result: 'passed', run_id: env.ADDP_ONLINE_TEST_RUN_ID, task_id: started.task_id,
      execution_id: started.execution_id, result_id: artifact.result_id, preview_url: artifact.preview_url,
      original_download_sha256: source.sha256, ordinary_user: true, task_page_loaded: true,
      cached_preview_loaded: true, generation_requests: generationRequests,
      browser_errors: 0, failed_business_responses: 0, gpu_diagnostics: gpu.snapshot()
    }))
  } catch (error) {
    const location = String(error.stack || '').match(/manager-sgm-local\.spec\.js:(\d+):(\d+)/)
    // Never retain error messages, DOM snapshots, model bytes or credentials.
    writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'sgm-browser-failure.json'), JSON.stringify({
      stage, source_line: location ? Number(location[1]) : null,
      generation_requests: generationRequests, browser_errors: errors.length,
      console_diagnostics: errors.slice(0, 20), failed_business_responses: failures
    }))
    throw error
  } finally {
    await api.dispose()
  }
})
