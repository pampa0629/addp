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

export function enviContentMetadata(meta, content, source) {
  const safe = meta?.attributes
  const expected = source.source_srid === null ? ['item', 'schema_version'] : ['capabilities', 'item', 'schema_version']
  if (!safe || JSON.stringify(Object.keys(safe).sort()) !== JSON.stringify(expected)
    || safe.schema_version !== 1 || JSON.stringify(Object.keys(safe.item || {}).sort()) !== JSON.stringify(['data_type', 'format', 'layout'])
    || safe.item.data_type !== 'media' || safe.item.format !== 'envi' || safe.item.layout !== 'multi') {
    throw new Error('ENVI Meta must contain only structural identity and declared CRS')
  }
  if (source.source_srid !== null) {
    const spatial = safe.capabilities?.spatial
    if (Object.keys(safe.capabilities).join() !== 'spatial' || spatial?.srid !== source.source_srid
      || Object.keys(spatial).some(key => !['srid', 'crs_ref', 'crs_definitions', 'geometry_columns', 'primary_geometry_column'].includes(key))) {
      throw new Error('ENVI Meta disclosed a content-derived spatial fact')
    }
  }
  const attrs = content?.data?.object?.attributes
  if (!attrs || attrs.item?.data_type !== 'media' || attrs.item?.format !== 'envi' || attrs.item?.layout !== 'multi'
    || attrs.format_info?.envi?.profile !== 'envi-standard-pair/v1' || attrs.format_info.envi.composition_complete !== true) {
    throw new Error('ENVI content metadata requires the current exact pair read')
  }
  return { ...meta, attributes: attrs }
}

async function tiffFixturePixelsMatch(frame) {
  return frame.locator('.tiff-canvas').evaluate(canvas => {
    if (canvas.width !== 240 || canvas.height !== 120) return false
    const rgba = canvas.getContext('2d').getImageData(0, 0, 240, 120).data
    for (let index = 0; index < 240 * 120; index++) {
      const missing = Math.floor(index / 240) < 5 && index % 240 < 7
      const gray = missing ? 0 : Math.round((index % 103) * 255 / 102)
      const offset = index * 4
      if (rgba[offset] !== gray || rgba[offset + 1] !== gray || rgba[offset + 2] !== gray ||
          rgba[offset + 3] !== (missing ? 0 : 255)) return false
    }
    return true
  })
}

test('IMG native metadata, COG pixels and cached reopen through the real Console', async ({ page }) => {
  test.setTimeout(240_000)
  const env = process.env
  const evidence = JSON.parse(env.ADDP_ONLINE_MANAGER_RASTER_MEDIA_JSON || '{}')
  expect(evidence.run_id).toBe(env.ADDP_ONLINE_TEST_RUN_ID)
  expect(['img', 'jpeg2000', 'grd', 'tiff', 'envi']).toContain(evidence.source_format)
  expect(evidence.cases.map(source => source.case)).toEqual({ img: ['wgs84', 'utm50n', 'unlocated'], jpeg2000: ['wgs84', 'utm50n', 'unlocated', 'codestream'], grd: ['v1', 'v2', 'surfer6', 'surfer6-ascii'], tiff: ['unlocated-small-cog'], envi: ['minio-bsq','minio-bil','minio-bip','nfs-bsq','nfs-bil','nfs-bip'] }[evidence.source_format])
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
    const isTiff = evidence.source_format === 'tiff'
    const rangeResponse = isTiff || source.source_srid !== null ? page.waitForResponse(response => {
      const url = new URL(response.url())
      const sourceMatches = isTiff
        ? url.pathname === '/api/v1/manager/storage-stream' && url.searchParams.get('locator') === source.locator
        : url.pathname === source.preview_url
      return sourceMatches && response.status() === 206 && Boolean(response.request().headers().range)
    }) : null
    rangeResponse?.catch(() => {})
    await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(source.locator)}`)
    const api = await browserAPIFromResponse(await sourcePreviewResponse, env.GATEWAY_URL, expectedIdentity)
    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const actual = { case: source.case, item_id: source.item_id, locator: source.locator,
      sha256: source.sha256, source_srid: source.source_srid, source_preview_verified: true }
    try {
      const structuralMeta = await json(await api.get(`/api/v1/meta/items/${source.item_id}`), 'IMG source metadata')
      const meta = evidence.source_format === 'envi'
        ? enviContentMetadata(structuralMeta, await json(await api.get('/api/v1/manager/preview', { params: { locator: source.locator } }), 'ENVI guarded content metadata'), source)
        : structuralMeta
      if (evidence.source_format === 'envi') {
        actual.metadata_projection_verified = true
        actual.content_metadata_verified = true
      }
      expect(meta.attributes.item).toMatchObject({ data_type: 'media', format: evidence.source_format, layout: evidence.source_format === 'envi' ? 'multi' : 'single' })
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
          const response = await rangeResponse
          expect(response.headers()['content-range']).toMatch(/^bytes \d+-\d+\/\d+$/)
          await expect.poll(() => tiffFixturePixelsMatch(frame)).toBe(true)
          actual.range_loaded = true
          actual.tiff_pixels_verified = true
          actual.tiff_pages_verified = true
        } else {
          await expect(frame.getByRole('button', { name: /^(?:生成 COG|Generate COG)$/, exact: true })).toHaveCount(0)
          await expect(frame.locator('.unsupported-preview')).toBeVisible()
          await expect(frame.locator('.image-preview')).toHaveCount(0)
          actual.generation_unavailable = true
        }
        await expect(frame.locator('.preview-panel img')).toHaveCount(0)
        actual.source_image_absent = true
        if (!isTiff) actual.unsupported_message_visible = true
        actual.map_absent = true
        await frame.locator('.preview-panel').screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `${evidence.source_format}-${source.case}.png`) })
        if (isTiff) {
          const reopened = page.waitForResponse(response => matchesSourcePreviewResponse(response, source.locator))
          await page.reload()
          const reopenedAPI = await browserAPIFromResponse(await reopened, env.GATEWAY_URL, expectedIdentity)
          const current = await json(await reopenedAPI.get(`/api/v1/meta/items/${source.item_id}`), 'TIFF reopened metadata')
          await reopenedAPI.dispose()
          expect(current).toEqual(meta)
          await expect.poll(() => tiffFixturePixelsMatch(frame)).toBe(true)
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
      const authenticatedSource = evidence.source_format === 'envi' ? page.waitForResponse(response => {
        const url = new URL(response.url())
        return response.request().method() === 'GET' && url.pathname === '/api/v1/manager/downloads/file'
          && url.searchParams.get('locator') === source.locator
      }) : null
      authenticatedSource?.catch(() => {})
      const downloadEvent = page.waitForEvent('download')
      await frame.getByRole('button', { name: /^(?:下载|Download)$/, exact: true }).click()
      const download = await downloadEvent
      expect(await download.failure()).toBeNull()
      if (authenticatedSource) {
        const response = await authenticatedSource
        expect(response.status()).toBe(200)
        expect(await response.finished()).toBeNull()
        const headers = await response.request().allHeaders()
        expect(/^Bearer addp_at_/.test(headers.authorization || '')).toBe(true)
        expect(Object.hasOwn(headers, 'cookie')).toBe(false)
        expect([...new URL(response.url()).searchParams.keys()]).toEqual(['locator'])
        const downloadAPI = await browserAPIFromResponse(response, env.GATEWAY_URL, expectedIdentity)
        await downloadAPI.dispose()
        expect(new URL(download.url()).protocol).toBe('blob:')
        actual.current_user_download_verified = true
      }
      const raw = readFileSync(await download.path())
      if (evidence.source_format === 'envi') {
        const code = `import sys,json,importlib
sys.path.insert(0,sys.argv[1])
owner=importlib.import_module('scripts.test.manager-internal-artifact-lineage-online')
with open(sys.argv[2],'rb')as content:raw=content.read(65537)
print(json.dumps(owner.validate_model_bundle(raw,json.loads(sys.argv[3]))))`
        actual.download_bundle = JSON.parse(execFileSync('python3', ['-c', code, resolve(process.cwd(), '../..'),
          await download.path(), JSON.stringify(source.download_members)], { encoding: 'utf8', timeout: 10000, maxBuffer: 65536 }))
        actual.download_sha256 = actual.download_bundle.members[source.name].sha256
        writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `envi-${source.case}-button.zip`), raw)
      } else {
        expect(raw.length).toBe(source.size_bytes)
        actual.download_sha256 = createHash('sha256').update(raw).digest('hex')
        if (isTiff) {
          const url = new URL(download.url())
          expect(url.pathname).toBe('/api/v1/manager/downloads/file')
          expect(url.searchParams.get('locator')).toBe(source.locator)
          writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'tiff-unlocated-small-cog-button.tif'), raw)
        }
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
