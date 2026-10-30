import { expect, test } from '@playwright/test'
import { createHash } from 'node:crypto'
import { readFileSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { execFileSync } from 'node:child_process'
import { modelPixels } from '../../../../manager/frontend/e2e/model-preview-support.js'
import { createBrowserGpuDiagnostics } from '../../../../common-frontend/basic/src/utils/browserGpuDiagnostics.mjs'
import { browserAPIFromResponse, isAnonymousRefreshConsoleError, isAnonymousRefreshResponse, json, login } from './transfer-browser-support.js'

export function matchesSourcePreviewResponse(response, locator) {
  const url = new URL(response.url())
  return response.request().method() === 'GET' && url.pathname === '/api/v1/manager/preview'
    && url.searchParams.get('locator') === locator
}

test('IMG native metadata, COG pixels and cached reopen through the real Console', async ({ page }) => {
  test.setTimeout(240_000)
  const env = process.env
  const evidence = JSON.parse(env.ADDP_ONLINE_MANAGER_RASTER_MEDIA_JSON || '{}')
  expect(evidence.run_id).toBe(env.ADDP_ONLINE_TEST_RUN_ID)
  expect(['img', 'jpeg2000', 'grd', 'tiff']).toContain(evidence.source_format)
  expect(evidence.cases.map(source => source.case)).toEqual({ img: ['wgs84', 'utm50n', 'unlocated'], jpeg2000: ['wgs84', 'utm50n', 'unlocated', 'codestream'], grd: ['v1', 'v2', 'surfer6', 'surfer6-ascii'], tiff: ['unlocated-small-cog'] }[evidence.source_format])
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
    const sourcePreviewResponse = page.waitForResponse(response => matchesSourcePreviewResponse(response, source.locator))
    sourcePreviewResponse.catch(() => {})
    const rangeResponse = source.source_srid === null ? null : page.waitForResponse(response =>
      new URL(response.url()).pathname === source.preview_url && response.status() === 206 && Boolean(response.request().headers().range))
    rangeResponse?.catch(() => {})
    await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(source.locator)}`)
    const api = await browserAPIFromResponse(await sourcePreviewResponse, env.GATEWAY_URL, expectedIdentity)
    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const actual = { case: source.case, item_id: source.item_id, locator: source.locator,
      sha256: source.sha256, source_srid: source.source_srid, source_preview_verified: true }
    try {
      const meta = await json(await api.get(`/api/v1/meta/items/${source.item_id}`), 'IMG source metadata')
      const isTiff = evidence.source_format === 'tiff'
      expect(meta.attributes.item).toMatchObject({ data_type: 'media', format: evidence.source_format, layout: isTiff ? 'multi' : 'single' })
      expect(meta.attributes.type_info.media).toMatchObject({ encoding: isTiff ? 'tiff' : source.driver, width: source.width ?? 256, height: source.height ?? 256 })
      if (isTiff) {
        expect(meta.attributes.format_info.tiff).toMatchObject({ profile: 'cog', is_cloud_optimized: true,
          cog_check_level: 'heuristic', is_tiled: true, has_overviews: false, big_tiff: false, page_count: 1,
          pages: [{ ifd_index: 0, width: 240, height: 120 }] })
        expect(meta.attributes.type_info.spatial).toBeUndefined()
        expect(meta.scanned_at).toBe(source.refresh.scanned_at_after)
        actual.scanned_at = meta.scanned_at
        actual.refreshed_metadata_verified = true
        actual.zero_overviews_verified = true
      } else {
        expect(meta.attributes.format_info[evidence.source_format]).toMatchObject({ scan_complete: true, band_count: source.band_count, driver: source.driver, ...source.grid_facts })
      }
      if (evidence.source_format === 'grd') {
        expect(meta.attributes.format_info.grd.bands).toHaveLength(1)
        expect(meta.attributes.format_info.grd.bands[0]).toMatchObject({ dtype: source.driver === 'GSBG' ? 'Float32' : 'Float64', nodata: source.grid_facts.blank_value, nodata_is_nan: false })
        if (['GSBG', 'GSAG'].includes(source.driver)) expect(meta.attributes.format_info.grd).not.toHaveProperty('version')
      }
      await frame.getByRole('tab', { name: /^(?:属性|Attributes)$/, exact: true }).click()
      if (isTiff) {
        await frame.getByRole('button', { name: /^(?:查看 JSON|View JSON)$/, exact: true }).click()
        const dialog = frame.getByRole('dialog')
        expect(JSON.parse(await dialog.locator('pre').innerText())).toEqual(meta.attributes)
        await dialog.getByRole('button', { name: /^(?:关闭此对话框|Close this dialog)$/, exact: true }).click()
        await expect(frame.getByText('cog', { exact: true }).first()).toBeVisible()
        await expect(frame.getByText('heuristic', { exact: true }).first()).toBeVisible()
        actual.cog_metadata_visible = true
        await frame.locator('.preview-container').screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'tiff-unlocated-small-cog-attributes.png') })
      } else {
        await expect(frame.getByText(source.driver, { exact: true }).first()).toBeVisible()
      }
      actual.metadata_verified = true
      await frame.getByRole('tab', { name: /^(?:预览|Preview)$/, exact: true }).click()
      if (source.source_srid === null) {
        expect(meta.attributes.capabilities?.spatial).toBeUndefined()
        await expect(frame.locator('.raster-tiff-quick-view')).toHaveCount(0)
        if (isTiff) {
          await expect(frame.locator('.tiff-pages')).toContainText('240 × 120')
          await expect(frame.locator('.tiff-pages')).toContainText(/第 1 页 \/ 共 1 页|Page 1 \/ 1/)
          await expect(frame.locator('.image-preview [role="status"]')).toContainText(/当前页的颜色或采样方式暂不支持基础预览|This page uses unsupported color or sample settings/)
          await expect(frame.locator('.tiff-canvas')).toBeHidden()
          actual.tiff_pages_verified = true
        } else {
          await expect(frame.getByRole('button', { name: /^(?:生成 COG|Generate COG)$/, exact: true })).toHaveCount(0)
          await expect(frame.locator('.unsupported-preview')).toBeVisible()
          await expect(frame.locator('.image-preview')).toHaveCount(0)
          actual.generation_unavailable = true
        }
        await expect(frame.locator('.preview-panel img')).toHaveCount(0)
        actual.source_image_absent = true
        actual.unsupported_message_visible = true
        actual.map_absent = true
        await frame.locator('.preview-panel').screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `${evidence.source_format}-${source.case}.png`) })
        if (isTiff) {
          const reopened = page.waitForResponse(response => matchesSourcePreviewResponse(response, source.locator))
          await page.reload()
          const reopenedAPI = await browserAPIFromResponse(await reopened, env.GATEWAY_URL, expectedIdentity)
          const current = await json(await reopenedAPI.get(`/api/v1/meta/items/${source.item_id}`), 'TIFF reopened metadata')
          await reopenedAPI.dispose()
          expect(current).toEqual(meta)
          await expect(frame.locator('.image-preview [role="status"]')).toContainText(/当前页的颜色或采样方式暂不支持基础预览|This page uses unsupported color or sample settings/)
          await expect(frame.locator('.raster-tiff-quick-view')).toHaveCount(0)
          actual.reopened = true
        }
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
      if (isTiff) {
        expect(new URL(download.url()).pathname).toBe('/api/v1/manager/downloads/file')
        expect(new URL(download.url()).searchParams.get('locator')).toBe(source.locator)
        const code = `import sys,json,importlib
sys.path.insert(0,sys.argv[1])
owner=importlib.import_module('scripts.test.manager-internal-artifact-lineage-online')
with open(sys.argv[2],'rb')as content:raw=content.read(65537)
print(json.dumps(owner.validate_model_bundle(raw,json.loads(sys.argv[3]))))`
        actual.download_bundle = JSON.parse(execFileSync('python3', ['-c', code, resolve(process.cwd(), '../..'),
          await download.path(), JSON.stringify(source.download_members)], { encoding: 'utf8', timeout: 10000, maxBuffer: 65536 }))
        actual.download_sha256 = actual.download_bundle.members['tiff-unlocated-small-cog.tif'].sha256
        writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'tiff-unlocated-small-cog-button.zip'), raw)
      } else {
        expect(raw.length).toBe(source.size_bytes)
        actual.download_sha256 = createHash('sha256').update(raw).digest('hex')
      }
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
    schema_version: 'addp.manager-raster-media-browser/v4', run_id: evidence.run_id, result: 'passed',
    source_format: evidence.source_format, identity: evidence.identity, cases, generation_requests: generationRequests,
    failed_business_responses: failures.length, warnings_errors: warnings.length,
    gpu_performance_diagnostics: diagnostics.snapshot()
  }) + '\n')
})
