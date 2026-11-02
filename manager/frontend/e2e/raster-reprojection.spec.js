import { expect, test } from '@playwright/test'
import { createBrowserGpuDiagnostics } from '../../../common-frontend/basic/src/utils/browserGpuDiagnostics.mjs'
import { writeArrayBuffer } from 'geotiff'
import { managerAuthContext } from './managerAuthContext.js'

for (const saved of [null, { center: [-73, 40], zoom: 8, rotation: 0 }]) {
  test(`raster initialization cannot publish a fallback or undo an explicit fit (saved=${Boolean(saved)})`, async ({ page }) => {
    const locator = 'addp://engine/2/path/deferred.tif?type=file&item_id=8'
    const rootLocator = 'addp://engine/2/path/?type=root&node_id=1'
    const item = { id: locator, locator, label: 'deferred.tif', type: 'file', hasChildren: false }
    const root = { id: rootLocator, locator: rootLocator, label: 'Raster source', type: 'root', hasChildren: true, children: [item] }
    const extent = [500000, 3097440, 502560, 3100000]
    const raw = Buffer.from(writeArrayBuffer(new Uint8Array(256 * 256 * 3).fill(128), {
      width: 256, height: 256, SamplesPerPixel: 3, BitsPerSample: [8, 8, 8], PhotometricInterpretation: 2,
      ModelPixelScale: [10, 10, 0], ModelTiepoint: [0, 0, 0, 500000, 3100000, 0],
      ProjectedCSTypeGeoKey: 32650, GTModelTypeGeoKey: 1, GTRasterTypeGeoKey: 1
    }))
    let release
    const ready = new Promise(resolve => { release = resolve })
    let reads = 0
    let view = saved ? { quick_view: { map: saved } } : {}
    await page.addInitScript(() => localStorage.setItem('addp-lang', 'en'))
    await page.route('**/*autonavi.com/**', route => route.abort())
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
        path: 'deferred.tif', attributes: { item: { data_type: 'media', format: 'tiff', layout: 'single' } }, content: { kind: 'unsupported', metadata: { format: 'tiff' } }
      } } })
      if (path.endsWith('/manager/preview-state/view-state')) { view = request.postDataJSON().view_state; return json({ view_state: view }) }
      if (path.endsWith('/manager/preview-state')) return json({ preferred_mode: 'map_quick_view', view_state: view })
      if (path.endsWith('/manager/quick-view/capability')) return json({ can_use_quick_view: true, preferred_mode: 'map_quick_view', view_state: view,
        render_source: 'client_cog_render', available_actions: ['back_to_basic_preview'],
        quick_view: { preview_url: '/api/v1/manager/raster_cog/1/content', extent, extent_srid: 32650 },
        raster: { profile: 'cog', width: 256, height: 256, band_count: 3 } })
      if (path.endsWith('/raster_cog/1/content')) {
        reads++
        if (reads > 1) await ready // Let the metadata probe finish; hold the actual source view.
        const range = /^bytes=(\d+)-(\d+)$/.exec(request.headers().range || '')
        if (!range) return route.fulfill({ status: 400 })
        const start = Number(range[1]), end = Math.min(Number(range[2]), raw.length - 1)
        return route.fulfill({ status: 206, headers: { 'Content-Type': 'image/tiff', 'Content-Range': `bytes ${start}-${end}/${raw.length}`, 'Accept-Ranges': 'bytes' }, body: raw.subarray(start, end + 1) })
      }
      return json({})
    })
    try {
      await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}`)
      const renderer = page.locator('.raster-tiff-quick-view')
      await expect(renderer).toBeVisible()
      await expect.poll(() => reads).toBeGreaterThanOrEqual(2)
      const initial = await renderer.evaluate(async element => {
        const instance = element.__vueParentComponent
        instance.setupState.map.renderSync()
        await instance.proxy.$nextTick()
        return instance.props.viewState
      })
      expect(initial).toEqual(saved || {})
      await renderer.getByRole('button', { name: /^(全幅显示|Fit to Extent)$/ }).click()
      release()
      await expect(renderer.locator('.loading-overlay')).toHaveCount(0)
      await expect.poll(() => renderer.evaluate(element => {
        const map = element.__vueParentComponent.setupState.map
        const center = map.getView().getCenter()
        return { inExtent: center[0] > 13024000 && center[0] < 13030000 && center[1] > 3200000 && center[1] < 3300000, zoom: map.getView().getZoom() }
      })).toMatchObject({ inExtent: true, zoom: expect.any(Number) })
      expect((await renderer.evaluate(element => element.__vueParentComponent.setupState.map.getView().getZoom()))).toBeGreaterThan(14)
    } finally { release() }
  })
}

test('raster data-tile reprojection preserves pixels without Canvas readback warnings', async ({ page }, testInfo) => {
  const gpuDiagnostics = createBrowserGpuDiagnostics('raster-reprojection')
  const diagnostics = []
  page.on('console', message => {
    if (gpuDiagnostics.record(message)) return
    if (['warning', 'error'].includes(message.type())) {
      diagnostics.push({ type: message.type(), text: message.text(), location: message.location() })
    }
  })
  page.on('pageerror', error => diagnostics.push({ type: 'pageerror', text: error.message }))
  await page.route('**/raster-reprojection-fixture', route => route.fulfill({
    contentType: 'text/html', body: '<!doctype html><html><body></body></html>'
  }))
  await page.goto('/raster-reprojection-fixture')
  const result = await page.evaluate(async () => {
    const { default: DataTileSource } = await import('/node_modules/ol/source/DataTile.js')
    const { get: getProjection } = await import('/node_modules/ol/proj.js')
    const reads = []
    const original = CanvasRenderingContext2D.prototype.getImageData
    CanvasRenderingContext2D.prototype.getImageData = function (...args) {
      reads.push({ attributes: this.getContextAttributes(), stack: new Error().stack })
      return original.apply(this, args)
    }
    const source = new DataTileSource({
      projection: 'EPSG:4326', tileSize: 32, bandCount: 1, maxZoom: 8, // The source grid must cover the finer resolution required by reprojection.
      loader: () => new Float32Array(32 * 32).fill(42)
    })
    const tiles = []
    try {
      for (const [z, x, y] of [[1, 0, 0], [1, 1, 0], [1, 0, 1], [1, 1, 1]]) {
        const tile = source.getTile(z, x, y, 1, getProjection('EPSG:3857'))
        await new Promise((resolve, reject) => {
          const finish = error => {
            clearTimeout(timeout)
            tile.removeEventListener('change', changed)
            if (error) reject(error)
            else resolve()
          }
          const changed = () => {
            if (tile.getState() === 2) finish()
            if (tile.getState() === 3) finish(tile.getError())
            if (tile.getState() === 4) finish(new Error('Reprojected tile is empty'))
          }
          const timeout = setTimeout(() => finish(new Error(`Tile loading timed out in state ${tile.getState()}`)), 8000)
          tile.addEventListener('change', changed)
          tile.load()
          changed()
        })
        const data = tile.getData()
        tiles.push({ length: data.length, hasValue: data.some(value => value === 42), validValues: data.every(value => value === 0 || value === 42) })
      }
    } finally {
      source.dispose()
      CanvasRenderingContext2D.prototype.getImageData = original
    }
    return { tiles, reads }
  })
  await page.waitForTimeout(500) // Drain Chromium console events before evaluating diagnostics.
  await testInfo.attach('gpu-performance-diagnostics', { body: JSON.stringify(gpuDiagnostics.snapshot()), contentType: 'application/json' })
  expect(result.reads).toEqual([])
  expect(result.tiles).toHaveLength(4)
  expect(result.tiles.every(tile => tile.length > 0 && tile.hasValue && tile.validValues)).toBe(true)
  expect(diagnostics, JSON.stringify(result.reads)).toEqual([])
})
