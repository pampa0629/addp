import { expect, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { rasterCompositionPixels, assertRasterCompositionPixels } from '../../../../manager/frontend/e2e/raster-composition-support.js'
import { createBrowserGpuDiagnostics } from '../../../../common-frontend/basic/src/utils/browserGpuDiagnostics.mjs'
import { browserAPIFromResponse, isAnonymousRefreshConsoleError, isAnonymousRefreshResponse, json, login } from './transfer-browser-support.js'

test('TIFF composition mask and location survive real Console cached reopen', async ({ page }) => {
  const env = process.env
  const evidence = JSON.parse(env.ADDP_ONLINE_MANAGER_RASTER_MEDIA_JSON || '{}')
  expect(evidence.run_id).toBe(env.ADDP_ONLINE_TEST_RUN_ID)
  expect(['msk', 'sidecars']).toContain(evidence.profile)
  const { source, profile } = evidence
  expect(source.preview_url).toBe(`/api/v1/manager/raster_cog/${source.result_id}/content`)
  const expectedIdentity = { principalID: evidence.identity.principal_id, tenantID: evidence.identity.tenant_id,
    permissions: new Set(evidence.identity.permissions_verified) }
  const diagnostics = createBrowserGpuDiagnostics('tiff-composition-login')
  let businessStarted = false, generationRequests = 0
  const warnings = [], failures = [], renders = []
  page.on('request', request => {
    const path = new URL(request.url()).pathname
    if (path === '/api/v1/system/auth/context' && request.headers().authorization) businessStarted = true
    if (path === '/api/v1/manager/quick-view/actions' && request.method() === 'POST') generationRequests++
  })
  page.on('console', message => {
    if (isAnonymousRefreshConsoleError(message, businessStarted) || diagnostics.record(message)) return
    if (['warning', 'error'].includes(message.type())) warnings.push(message.text())
  })
  page.on('pageerror', error => warnings.push(error.message))
  page.on('response', response => {
    if (!isAnonymousRefreshResponse(response, businessStarted) && new URL(response.url()).pathname.startsWith('/api/v1/') && response.status() >= 400) failures.push(response.status())
  })
  await login(page, env.ADDP_ONLINE_TEST_USER_USERNAME, env.ADDP_ONLINE_TEST_USER_PASSWORD, '/')
  for (const stage of ['open', 'reopen']) {
    diagnostics.setStage(`${profile}:${stage}`)
    const capability = page.waitForResponse(response => {
      const url = new URL(response.url())
      return url.pathname === '/api/v1/manager/quick-view/capability' && url.searchParams.get('locator') === source.locator
    })
    const ranged = page.waitForResponse(response => new URL(response.url()).pathname === source.preview_url && Boolean(response.request().headers().range))
    capability.catch(() => {}); ranged.catch(() => {})
    if (stage === 'open') await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(source.locator)}`)
    else await page.reload()
    const response = await capability
    const api = await browserAPIFromResponse(response, env.GATEWAY_URL, expectedIdentity)
    try {
      const ready = await json(response, 'TIFF composition cached capability')
      expect(ready.render_source).toBe('client_cog_render')
      expect(ready.quick_view.preview_url).toBe(source.preview_url)
      expect(ready.quick_view.extent).toHaveLength(4)
      ready.quick_view.extent.forEach((value, index) => expect(value).toBeCloseTo([113, 27.9744, 113.0256, 28][index], 8))
      expect(ready.quick_view.extent_srid).toBe(4326)
      const range = await ranged
      expect(range.status()).toBe(206)
      expect(range.headers()['content-range']).toMatch(new RegExp(`^bytes \\d+-\\d+/${source.size_bytes}$`))
      expect((await range.body()).length).toBeGreaterThan(0)
      const preview = page.frameLocator('iframe[data-testid="module-iframe"]').locator('.raster-tiff-quick-view')
      await expect(preview).toBeVisible()
      await preview.getByRole('button', { name: /^(?:全幅显示|Fit to Extent)$/, exact: true }).click()
      await expect(preview.locator('.loading-overlay')).toHaveCount(0)
      await expect(preview.locator('.map-empty')).toHaveCount(0)
      let render
      try {
        await expect.poll(async () => {
          render = await rasterCompositionPixels(preview, profile)
          try { assertRasterCompositionPixels(render, profile); return true } catch { return false }
        }, { message: `${profile} actual canvas transparency and location` }).toBe(true)
      } finally {
        writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `tiff-${profile}-${stage}-rgba.json`), JSON.stringify({
          run_id: evidence.run_id, profile, identity: evidence.identity, source, render: render ?? null
        }) + '\n')
      }
      await preview.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `tiff-${profile}-${stage}.png`) })
      renders.push(render)
    } finally { await api.dispose() }
  }
  expect(generationRequests).toBe(0)
  expect(warnings).toEqual([])
  expect(failures).toEqual([])
  writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `manager-tiff-${profile}-browser.json`), JSON.stringify({
    schema_version: 'addp.manager-tiff-composition-browser/v1', result: 'passed', run_id: evidence.run_id,
    profile, identity: evidence.identity, source, range_status: 206, renders, reopened: true, reused_result: true,
    generation_requests: generationRequests, failed_business_responses: failures.length, warnings_errors: warnings.length,
    gpu_performance_diagnostics: diagnostics.snapshot()
  }) + '\n')
})
