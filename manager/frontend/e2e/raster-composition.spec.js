import { expect, test } from '@playwright/test'
import { createBrowserGpuDiagnostics } from '../../../common-frontend/basic/src/utils/browserGpuDiagnostics.mjs'
import { readFileSync } from 'node:fs'
import { createHash } from 'node:crypto'
import { rasterCompositionPixels, assertRasterCompositionPixels } from './raster-composition-support.js'
import { managerAuthContext } from './managerAuthContext.js'

// Synthetic 256x256 COGs from run 43422110359; no personal source data.
const hashes = { msk: 'e78b78966acc324a8087beb7dea50604522d5a37a5f5a7a7cb4473cea671ffde', sidecars: '9f221ece15de49ecd85f5f00da28f25f983e5afe2f417ff2dfefd086952a1b55' }
for (const profile of ['msk', 'sidecars']) {
  test(`TIFF ${profile} renders mask and auxiliary grid`, async ({ page }, testInfo) => {
    const diagnostics = createBrowserGpuDiagnostics(`tiff-${profile}`)
    const warnings = []
    page.on('console', message => {
      if (!diagnostics.record(message) && ['warning', 'error'].includes(message.type())) warnings.push(message.text())
    })
    page.on('pageerror', error => warnings.push(error.message))
    const locator = 'addp://engine/2/path/deferred.tif?type=file&item_id=8'
    const rootLocator = 'addp://engine/2/path/?type=root&node_id=1'
    const item = { id: locator, locator, label: 'deferred.tif', type: 'file', hasChildren: false }
    const root = { id: rootLocator, locator: rootLocator, label: 'Raster source', type: 'root', hasChildren: true, children: [item] }
    const extent = [113, 27.9744, 113.0256, 28]
    const raw = readFileSync(new URL(`./fixtures/tiff-${profile}.cog.tif`, import.meta.url))
    expect(createHash('sha256').update(raw).digest('hex')).toBe(hashes[profile])
    let reads = 0
    let view = {}
    await page.addInitScript(() => localStorage.setItem('addp-lang', 'en'))
    await page.route('**/*autonavi.com/**', route => route.fulfill({ contentType: 'image/png', headers: { 'access-control-allow-origin': '*' }, body: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==', 'base64') }))
    await page.route('**/api/v1/**', async route => {
      const request = route.request(), path = new URL(request.url()).pathname
      const json = value => route.fulfill({ json: value })
      if (path.endsWith('/system/refresh')) return json({ access_token: 'raster-view-fixture', expires_in: 3600 })
      if (path.endsWith('/system/users/me')) return json({ id: 1, display_name: 'raster-view', local_account: { username: 'raster-view' } })
      if (path.endsWith('/system/auth/context')) return json(managerAuthContext)
      if (path.endsWith('/manager/engines')) return json({ data: [{ id: 2, name: root.label, engine_type: 'nfs', lifecycle_state: 'active', connection_status: 'online' }] })
      if (path.endsWith('/ancestors')) return json({ target_locator: locator, ancestors: [root, item] })
      if (path.endsWith('/meta/resource-tree/2') || path.endsWith('/node')) return json(root)
      if (path.endsWith('/manager/preview')) return json({ preview_type: 'object', data: { mode: 'object', object: {
        path: 'deferred.tif', attributes: { item: { data_type: 'media', format: 'tiff', layout: 'multi' } }, content: { kind: 'unsupported', metadata: { format: 'tiff' } }
      } } })
      if (path.endsWith('/manager/preview-state/view-state')) { view = request.postDataJSON().view_state; return json({ view_state: view }) }
      if (path.endsWith('/manager/preview-state')) return json({ preferred_mode: 'map_quick_view', view_state: view })
      if (path.endsWith('/manager/quick-view/capability')) return json({ can_use_quick_view: true, preferred_mode: 'map_quick_view', view_state: view,
        render_source: 'client_cog_render', available_actions: ['back_to_basic_preview'],
        quick_view: { preview_url: '/api/v1/manager/raster_cog/1/content', extent, extent_srid: 4326 },
        raster: { profile: 'cog', width: 256, height: 256, band_count: 1 } })
      if (path.endsWith('/raster_cog/1/content')) {
        reads++
        const range = /^bytes=(\d+)-(\d+)$/.exec(request.headers().range || '')
        if (!range) return route.fulfill({ status: 400 })
        const start = Number(range[1]), end = Math.min(Number(range[2]), raw.length - 1)
        return route.fulfill({ status: 206, headers: { 'Content-Type': 'image/tiff', 'Content-Range': `bytes ${start}-${end}/${raw.length}`, 'Accept-Ranges': 'bytes' }, body: raw.subarray(start, end + 1) })
      }
      return json({})
    })
    await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}`)
    const preview = page.locator('.raster-tiff-quick-view')
    await expect(preview).toBeVisible()
    await preview.getByRole('button', { name: /^(全幅显示|Fit to Extent)$/ }).click()
    await expect(preview.locator('.loading-overlay')).toHaveCount(0)
    let proof
    try {
      await expect.poll(async () => {
        proof = await rasterCompositionPixels(preview, profile)
        try { assertRasterCompositionPixels(proof, profile); return true } catch { return false }
      }, { message: 'Actual TIFF canvas mask and grid', timeout: 15000 }).toBe(true)
    } finally {
      await testInfo.attach('rendered-mask-grid', { body: JSON.stringify(proof ?? null), contentType: 'application/json' })
    }
    expect(reads).toBeGreaterThan(0)
    await testInfo.attach('map', { body: await preview.screenshot(), contentType: 'image/png' })
    // Reopen through the real Manager entry; use the same assertion as T4.
    await page.reload()
    await expect(preview).toBeVisible()
    await preview.getByRole('button', { name: /^(全幅显示|Fit to Extent)$/ }).click()
    await expect.poll(async () => {
      try { assertRasterCompositionPixels(await rasterCompositionPixels(preview, profile), profile); return true } catch { return false }
    }).toBe(true)
    // Force the real shader opaque: the same pixel oracle must reject it.
    if (profile === 'msk') {
      await preview.evaluate(element => {
        const layer = element.__vueParentComponent.setupState.map.getLayers().getArray().at(-1)
        layer.setStyle({ color: [128, 128, 128, 1] })
      })
      await expect.poll(async () => (await rasterCompositionPixels(preview, profile)).pixels[0].rgba[3]).toBeGreaterThan(200)
      const opaque = await rasterCompositionPixels(preview, profile)
      expect(() => assertRasterCompositionPixels(opaque, profile)).toThrow(/transparency/)
    }
    await preview.evaluate(element => element.__vueParentComponent.setupState.map.getView().setCenter([0, 0]))
    const displaced = await rasterCompositionPixels(preview, profile)
    expect(() => assertRasterCompositionPixels(displaced, profile)).toThrow(/differs/)
    expect(warnings).toEqual([])
    await testInfo.attach('gpu-performance-diagnostics', { body: JSON.stringify(diagnostics.snapshot()), contentType: 'application/json' })
  })
}
