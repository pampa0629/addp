import { expect, request, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { isAnonymousRefreshConsoleError, isAnonymousRefreshResponse, json, login } from './transfer-browser-support.js'

const requiredNames = [
  'ADDP_ONLINE_ARTIFACT_DIR',
  'ADDP_ONLINE_TEST_RUN_ID',
  'ADDP_ONLINE_TEST_TENANT_ID',
  'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN',
  'ADDP_ONLINE_TEST_USER_USERNAME',
  'ADDP_ONLINE_TEST_USER_PASSWORD',
  'ADDP_ONLINE_MANAGER_LINEAGE_EXECUTION_ID',
  'ADDP_ONLINE_MANAGER_LINEAGE_ITEM_ID',
  'ADDP_ONLINE_MANAGER_LINEAGE_SOURCE_NAME',
  'ADDP_ONLINE_MANAGER_LINEAGE_OUTPUT_NAME',
  'ADDP_ONLINE_MANAGER_PPTX_ITEM_LOCATOR',
  'ADDP_ONLINE_MANAGER_PPTX_ITEM_ID',
  'ADDP_ONLINE_MANAGER_PPTX_PAGE_COUNT',
  'ADDP_ONLINE_MANAGER_MODELS_JSON',
  'ADDP_ONLINE_MANAGER_BROWSER_PHASE',
  'ADDP_ONLINE_MANAGER_RASTER_JSON',
  'GATEWAY_URL'
]

function environment() {
  const missing = requiredNames.filter(name => !process.env[name])
  if (missing.length > 0) throw new Error(`missing Online environment: ${missing.join(', ')}`)
  return Object.fromEntries(requiredNames.map(name => [name, process.env[name]]))
}

test('Manager lineage, cached PPTX, direct COG and textured DAE/3DS GLB load through Console', async ({ page }) => {
  const env = environment()
  const itemID = Number(env.ADDP_ONLINE_MANAGER_LINEAGE_ITEM_ID)
  if (!Number.isInteger(itemID) || itemID <= 0) throw new Error('Manager lineage item ID must be positive')
  const pptxItemID = Number(env.ADDP_ONLINE_MANAGER_PPTX_ITEM_ID)
  const pptxPageCount = Number(env.ADDP_ONLINE_MANAGER_PPTX_PAGE_COUNT)
  if (!Number.isInteger(pptxItemID) || pptxItemID <= 0) throw new Error('Manager PPTX item ID must be positive')
  if (pptxPageCount !== 3) throw new Error('Manager PPTX fixture must have exactly 3 pages')
  test.setTimeout(240_000)
  const raster = JSON.parse(env.ADDP_ONLINE_MANAGER_RASTER_JSON)
  expect(Number.isInteger(raster.item_id) && raster.item_id > 0).toBe(true)
  expect(raster.preview_url).toMatch(/^\/api\/v1\/manager\/raster_cog\/[1-9][0-9]*\/content$/)
  const phase = env.ADDP_ONLINE_MANAGER_BROWSER_PHASE
  expect(['generation-entry', 'cached-preview']).toContain(phase)
  const models = JSON.parse(env.ADDP_ONLINE_MANAGER_MODELS_JSON)
  expect(models.map(model => model.format)).toEqual(['dae', '3ds'])
  for (const model of models) {
    expect(Number.isInteger(model.item_id) && model.item_id > 0).toBe(true)
    if (phase === 'generation-entry') continue
    expect(Number.isInteger(model.result_id) && model.result_id > 0).toBe(true)
    expect(model.preview_url).toBe(`/api/v1/manager/model_3d_glb/${model.result_id}/content`)
  }
  const executionPath = `/monitor/executions?execution_id=${encodeURIComponent(env.ADDP_ONLINE_MANAGER_LINEAGE_EXECUTION_ID)}`
  const api = await request.newContext({
    baseURL: env.GATEWAY_URL,
    extraHTTPHeaders: { Authorization: `Bearer ${env.ADDP_ONLINE_TEST_USER_ACCESS_TOKEN}` }
  })
  const browserMessages = []
  let gpuPerformanceWarnings = 0
  const failedBusinessResponses = []
  let businessStarted = false
  let anonymousRefresh401 = 0
  let pptxGenerationRequests = 0
  let modelGenerationRequests = 0
  let rasterGenerationRequests = 0
  let managerEngineRequests = 0
  page.on('request', requestEvent => {
    const pathname = new URL(requestEvent.url()).pathname
    if (pathname === '/api/v1/system/auth/context' && requestEvent.headers().authorization) {
      businessStarted = true
    }
    if (requestEvent.method() === 'POST' && pathname === '/api/v1/manager/quick-view/actions') {
      const action = requestEvent.postDataJSON()?.action
      if (action === 'generate_pptx_pdf') pptxGenerationRequests += 1
      if (action === 'generate_raster_cog') rasterGenerationRequests += 1
      if (action === 'generate_model_3d_glb') modelGenerationRequests += 1
    }
    if (requestEvent.method() === 'GET' && pathname === '/api/v1/manager/engines') {
      managerEngineRequests += 1
    }
  })
  page.on('console', message => {
    if (isAnonymousRefreshConsoleError(message, businessStarted)) return
    if (message.type() === 'warning' && /^\[\.WebGL-0x[0-9a-f]+\]GL Driver Message \(OpenGL, Performance, GL_CLOSE_PATH_NV, High\): GPU stall due to ReadPixels(?: \(this message will no longer repeat\))?$/.test(message.text())) {
      gpuPerformanceWarnings += 1
      return
    }
    if (['warning', 'error'].includes(message.type())) {
      browserMessages.push({ type: message.type(), text: message.text() })
    }
  })
  page.on('pageerror', error => browserMessages.push({ type: 'pageerror', text: error.message }))
  page.on('response', response => {
    const pathname = new URL(response.url()).pathname
    if (isAnonymousRefreshResponse(response, businessStarted)) {
      anonymousRefresh401 += 1
      return
    }
    if (pathname.startsWith('/api/v1/') && response.status() >= 400) {
      failedBusinessResponses.push({ pathname, status: response.status() })
    }
  })

  try {
    const apiIdentity = await json(await api.get('/api/v1/system/auth/context'), 'read API AuthContext')
    expect(apiIdentity?.principal?.type).toBe('user')
    expect(String(apiIdentity?.context?.tenant_id)).toBe(env.ADDP_ONLINE_TEST_TENANT_ID)

    const browserAccessToken = await login(
      page,
      env.ADDP_ONLINE_TEST_USER_USERNAME,
      env.ADDP_ONLINE_TEST_USER_PASSWORD,
      executionPath
    )
    businessStarted = true
    const browserAPI = await request.newContext({
      baseURL: env.GATEWAY_URL,
      extraHTTPHeaders: { Authorization: `Bearer ${browserAccessToken}` }
    })
    const browserIdentity = await json(await browserAPI.get('/api/v1/system/auth/context'), 'read browser AuthContext')
    expect(String(browserIdentity?.principal?.id)).toBe(String(apiIdentity?.principal?.id))
    expect(String(browserIdentity?.context?.tenant_id)).toBe(env.ADDP_ONLINE_TEST_TENANT_ID)
    await browserAPI.dispose()

    if (phase === 'generation-entry') {
      const modelEvidence = []
      for (const model of models) {
        await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(model.locator)}`)
        const modelFrame = page.frameLocator('iframe[data-testid="module-iframe"]')
        const generate = modelFrame.getByRole('button', { name: /生成 GLB 快显|Generate GLB Quick View/ })
        await expect(generate).toBeVisible()
        await expect(generate).toBeEnabled()
        await expect(modelFrame.locator('.model-preview')).toHaveCount(0)
        await modelFrame.locator('.preview-panel').screenshot({
          path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `model-${model.format}-generation-entry.png`)
        })
        modelEvidence.push({ ...model, generation_entry_visible: true })
      }
      expect(modelGenerationRequests).toBe(0)
      expect(failedBusinessResponses).toEqual([])
      expect(browserMessages).toEqual([])
      expect(anonymousRefresh401).toBeLessThanOrEqual(1)
      writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `manager-internal-artifact-lineage-browser-${phase}.json`),
        `${JSON.stringify({
          schema_version: 'addp.manager-internal-artifact-lineage-browser/v4', phase,
          suite: 'manager-internal-artifact-lineage', run_id: env.ADDP_ONLINE_TEST_RUN_ID,
          result: 'passed', models: modelEvidence, model_generation_requests: modelGenerationRequests,
          browser_warning_errors: 0, failed_business_responses: 0,
          anonymous_refresh_401: anonymousRefresh401, gpu_performance_warnings: gpuPerformanceWarnings
        })}\n`, 'utf8')
      return
    }

    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const lineage = frame.locator('.execution-lineage')
    await expect(lineage).toBeVisible()
    const groups = lineage.locator('.execution-lineage__group')
    await expect(groups).toHaveCount(2)
    const inputCards = groups.nth(0).locator('.execution-lineage__card')
    const outputCards = groups.nth(1).locator('.execution-lineage__card')
    await expect(inputCards).toHaveCount(1)
    await expect(outputCards).toHaveCount(1)
    await expect(inputCards.first()).toContainText(env.ADDP_ONLINE_MANAGER_LINEAGE_SOURCE_NAME)
    await expect(inputCards.first()).toContainText(String(itemID))
    await expect(inputCards.first().locator('.execution-lineage__resource-action')).toHaveCount(1)
    await expect(outputCards.first()).toContainText(env.ADDP_ONLINE_MANAGER_LINEAGE_OUTPUT_NAME)
    await expect(outputCards.first()).toContainText(/平台内部产物|Platform-internal artifact/)
    await expect(outputCards.first().locator('.execution-lineage__resource-action')).toHaveCount(0)

    const dataExplorerPath = `/manager/data-explorer?locator=${encodeURIComponent(env.ADDP_ONLINE_MANAGER_PPTX_ITEM_LOCATOR)}`
    await page.goto(dataExplorerPath)
    const explorerFrame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const pdfPreview = explorerFrame.locator('.pptx-preview .pdf-preview')
    await expect(pdfPreview).toBeVisible({ timeout: 60_000 })
    await expect(pdfPreview.locator('.page-info')).toContainText(`/ ${pptxPageCount}`)
    await pdfPreview.locator('.toolbar-left .el-button-group .el-button').nth(1).click()
    const currentPageInput = pdfPreview.locator('.page-info input')
    await expect(currentPageInput).toHaveValue('2')
    const engineRequestsAfterPreviewReady = managerEngineRequests
    await expect.poll(() => managerEngineRequests, { timeout: 25_000 }).toBeGreaterThan(engineRequestsAfterPreviewReady)
    await expect(currentPageInput).toHaveValue('2')
    expect(pptxGenerationRequests).toBe(0)

    const cogResponse = page.waitForResponse(response =>
      new URL(response.url()).pathname === raster.preview_url && response.status() === 206
      && Boolean(response.request().headers().range)
    )
    await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(raster.locator)}`)
    const rasterFrame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const rasterPreview = rasterFrame.locator('.raster-tiff-quick-view')
    await expect(rasterPreview).toBeVisible({ timeout: 60_000 })
    await expect(rasterPreview.locator('canvas')).toBeVisible()
    const cogRange = await cogResponse
    expect((await cogRange.body()).length).toBeGreaterThan(0)
    expect(cogRange.headers()['content-range']).toMatch(/^bytes [0-9]+-[0-9]+\/[0-9]+$/)
    await expect(rasterPreview.locator('.loading-overlay')).toHaveCount(0, { timeout: 60_000 })
    await expect(rasterPreview.locator('.map-empty')).toHaveCount(0)
    await rasterPreview.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'raster-cog-preview.png') })
    expect(rasterGenerationRequests).toBe(0)

    const modelEvidence = []
    for (const model of models) {
      const contentResponse = page.waitForResponse(response =>
        new URL(response.url()).pathname === model.preview_url && response.status() === 200
      )
      await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(model.locator)}`)
      const modelFrame = page.frameLocator('iframe[data-testid="module-iframe"]')
      const preview = modelFrame.locator('.model-preview')
      await expect(preview).toBeVisible({ timeout: 60_000 })
      await expect(preview.locator('canvas')).toBeVisible()
      const response = await contentResponse
      expect((await response.body()).subarray(0, 4).toString('ascii')).toBe('glTF')
      await expect(preview.locator('.three-status')).toHaveCount(0, { timeout: 60_000 })
      await expect(modelFrame.getByRole('button', { name: /生成 GLB 快显|Generate GLB Quick View/ })).toHaveCount(0)
      await preview.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `model-${model.format}-preview.png`) })
      modelEvidence.push({ ...model, model_loaded: true, content_loaded: true })
    }
    expect(modelGenerationRequests).toBe(0)
    expect(failedBusinessResponses).toEqual([])
    expect(browserMessages).toEqual([])
    expect(anonymousRefresh401).toBeLessThanOrEqual(1)

    const report = {
      schema_version: 'addp.manager-internal-artifact-lineage-browser/v4',
      phase,
      suite: 'manager-internal-artifact-lineage',
      run_id: env.ADDP_ONLINE_TEST_RUN_ID,
      result: 'passed',
      execution_id: env.ADDP_ONLINE_MANAGER_LINEAGE_EXECUTION_ID,
      item_id: itemID,
      output_name: env.ADDP_ONLINE_MANAGER_LINEAGE_OUTPUT_NAME,
      input_resources: 1,
      output_resources: 1,
      platform_internal_outputs: 1,
      pptx_item_id: pptxItemID,
      pptx_page_count: pptxPageCount,
      pptx_page_after_engine_refresh: 2,
      pptx_generation_requests: pptxGenerationRequests,
      model_generation_requests: modelGenerationRequests,
      raster_generation_requests: rasterGenerationRequests,
      raster: { ...raster, range_loaded: true, map_loaded: true },
      models: modelEvidence,
      gpu_performance_warnings: gpuPerformanceWarnings,
      browser_warning_errors: 0,
      failed_business_responses: 0,
      anonymous_refresh_401: anonymousRefresh401
    }
    writeFileSync(
      resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `manager-internal-artifact-lineage-browser-${phase}.json`),
      `${JSON.stringify(report)}\n`,
      'utf8'
    )
  } finally {
    await api.dispose()
  }
})
