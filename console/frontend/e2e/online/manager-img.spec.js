import { expect, test } from '@playwright/test'
import { createHash } from 'node:crypto'
import { readFileSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { modelPixels } from '../../../../manager/frontend/e2e/model-preview-support.js'
import { createBrowserGpuDiagnostics } from '../../../../common-frontend/basic/src/utils/browserGpuDiagnostics.mjs'
import { browserAPIFromResponse, isAnonymousRefreshConsoleError, isAnonymousRefreshResponse, json, login } from './transfer-browser-support.js'

test('IMG native metadata, COG pixels and cached reopen through the real Console', async ({ page }) => {
  test.setTimeout(240_000)
  const env = process.env
  const evidence = JSON.parse(env.ADDP_ONLINE_MANAGER_RASTER_MEDIA_JSON || '{}')
  expect(evidence.run_id).toBe(env.ADDP_ONLINE_TEST_RUN_ID)
  expect(['img', 'jpeg2000', 'grd']).toContain(evidence.source_format)
  expect(evidence.cases.map(source => source.case)).toEqual({ img: ['wgs84', 'utm50n', 'unlocated'], jpeg2000: ['wgs84', 'utm50n', 'unlocated', 'codestream'], grd: ['v1', 'v2'] }[evidence.source_format])
  const expectedIdentity = { principalID: evidence.identity.principal_id, tenantID: evidence.identity.tenant_id,
    permissions: new Set(evidence.identity.permissions_verified) }
  const diagnostics = createBrowserGpuDiagnostics('img-login')
  let businessStarted = false
  let generationRequests = 0
  const warnings = []
  const failures = []
  page.on('request', event => {
    const path = new URL(event.url()).pathname
    if (path === '/api/v1/system/auth/context' && event.headers().authorization) businessStarted = true
    if (path === '/api/v1/manager/quick-view/actions' && event.method() === 'POST') generationRequests++
  })
  page.on('console', message => {
    if (isAnonymousRefreshConsoleError(message, businessStarted) || diagnostics.record(message)) return
    if (['warning', 'error'].includes(message.type())) warnings.push({ type: message.type(), message: message.text() })
  })
  page.on('pageerror', error => warnings.push({ type: 'pageerror', message: error.message }))
  page.on('response', response => {
    if (isAnonymousRefreshResponse(response, businessStarted)) return
    const path = new URL(response.url()).pathname
    if (path.startsWith('/api/v1/') && response.status() >= 400) failures.push({ path, status: response.status() })
  })
  await login(page, env.ADDP_ONLINE_TEST_USER_USERNAME, env.ADDP_ONLINE_TEST_USER_PASSWORD, '/')
  const cases = []
  for (const source of evidence.cases) {
    diagnostics.setStage(`img-${source.case}:preview`)
    const capabilityResponse = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/manager/quick-view/capability'
      && new URL(response.url()).searchParams.get('locator') === source.locator)
    capabilityResponse.catch(() => {})
    const rangeResponse = source.source_srid === null ? null : page.waitForResponse(response =>
      new URL(response.url()).pathname === source.preview_url && response.status() === 206 && Boolean(response.request().headers().range))
    rangeResponse?.catch(() => {})
    await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(source.locator)}`)
    const api = await browserAPIFromResponse(await capabilityResponse, env.GATEWAY_URL, expectedIdentity)
    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const actual = { case: source.case, item_id: source.item_id, locator: source.locator,
      sha256: source.sha256, source_srid: source.source_srid }
    try {
      const meta = await json(await api.get(`/api/v1/meta/items/${source.item_id}`), 'IMG source metadata')
      expect(meta.attributes.item).toMatchObject({ data_type: 'media', format: evidence.source_format, layout: 'single' })
      expect(meta.attributes.type_info.media).toMatchObject({ encoding: source.driver, width: source.width ?? 256, height: source.height ?? 256 })
      expect(meta.attributes.format_info[evidence.source_format]).toMatchObject({ scan_complete: true, band_count: source.band_count, driver: source.driver, ...source.grid_facts })
      await frame.getByRole('tab', { name: /^(?:属性|Attributes)$/, exact: true }).click()
      await expect(frame.getByText(source.driver, { exact: true }).first()).toBeVisible()
      actual.metadata_verified = true
      await frame.getByRole('tab', { name: /^(?:预览|Preview)$/, exact: true }).click()
      if (source.source_srid === null) {
        expect(meta.attributes.capabilities?.spatial).toBeUndefined()
        await expect(frame.getByRole('button', { name: /^(?:生成 COG|Generate COG)$/, exact: true })).toHaveCount(0)
        await expect(frame.locator('.raster-tiff-quick-view')).toHaveCount(0)
        await expect(frame.locator('.unsupported-preview')).toBeVisible()
        await expect(frame.locator('.image-preview')).toHaveCount(0)
        await expect(frame.locator('.preview-panel img')).toHaveCount(0)
        actual.source_image_absent = true
        actual.unsupported_message_visible = true
        actual.generation_unavailable = true
        actual.map_absent = true
        await frame.locator('.preview-panel').screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `${evidence.source_format}-${source.case}.png`) })
      } else {
        expect(meta.attributes.capabilities.spatial.srid).toBe(source.source_srid)
        const response = await rangeResponse
        expect((await response.body()).length).toBeGreaterThan(0)
        expect(response.headers()['content-range']).toMatch(/^bytes \d+-\d+\/\d+$/)
        const preview = frame.locator('.raster-tiff-quick-view')
        await expect(preview).toBeVisible()
        await preview.getByRole('button', { name: /^(?:全幅显示|Fit to Extent)$/, exact: true }).click()
        await expect(preview.locator('.loading-overlay')).toHaveCount(0)
        await expect(preview.locator('.map-empty')).toHaveCount(0)
        let pixels
        const checkPixels = async () => {
          pixels = await modelPixels(page, preview.locator('canvas.ol-layer'))
          return Math.min(pixels.cyan, pixels.magenta, pixels.yellow)
        }
        await expect.poll(checkPixels).toBeGreaterThanOrEqual(128)
        actual.pixels = { cyan: pixels.cyan, magenta: pixels.magenta, yellow: pixels.yellow }
        actual.range_loaded = true
        actual.map_loaded = true
        actual.preview_url = source.preview_url
        diagnostics.setStage(`img-${source.case}:screenshot`)
        await preview.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `${evidence.source_format}-${source.case}.png`) })
        diagnostics.setStage(`img-${source.case}:reopen`)
        const reloaded = page.waitForResponse(result => new URL(result.url()).pathname === '/api/v1/manager/quick-view/capability'
          && new URL(result.url()).searchParams.get('locator') === source.locator)
        reloaded.catch(() => {})
        await page.reload()
        const reopenedResponse = await reloaded
        const reopenedAPI = await browserAPIFromResponse(reopenedResponse, env.GATEWAY_URL, expectedIdentity)
        await reopenedAPI.dispose()
        const capability = await json(reopenedResponse, 'IMG cached capability')
        expect(capability.quick_view.preview_url).toBe(source.preview_url)
        await expect(preview).toBeVisible()
        await preview.getByRole('button', { name: /^(?:全幅显示|Fit to Extent)$/, exact: true }).click()
        await expect.poll(checkPixels).toBeGreaterThanOrEqual(128)
        actual.reopened = true
        actual.reused_result = true
      }
      const downloadEvent = page.waitForEvent('download')
      await frame.getByRole('button', { name: /^(?:下载|Download)$/, exact: true }).click()
      const download = await downloadEvent
      expect(await download.failure()).toBeNull()
      const raw = readFileSync(await download.path())
      expect(raw.length).toBe(source.size_bytes)
      actual.download_sha256 = createHash('sha256').update(raw).digest('hex')
      expect(actual.download_sha256).toBe(source.sha256)
      cases.push(actual)
    } finally {
      await api.dispose()
    }
  }
  expect(generationRequests).toBe(0)
  expect(warnings).toEqual([])
  expect(failures).toEqual([])
  writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `manager-${evidence.source_format}-browser.json`), JSON.stringify({
    schema_version: 'addp.manager-raster-media-browser/v2', run_id: evidence.run_id, result: 'passed',
    source_format: evidence.source_format, identity: evidence.identity, cases, generation_requests: generationRequests,
    failed_business_responses: failures.length, warnings_errors: warnings.length,
    gpu_performance_diagnostics: diagnostics.snapshot()
  }) + '\n')
})
