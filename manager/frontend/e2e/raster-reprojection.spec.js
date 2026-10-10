import { expect, test } from '@playwright/test'

test('raster data-tile reprojection preserves pixels without Canvas readback warnings', async ({ page }) => {
  const diagnostics = []
  page.on('console', message => {
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
      projection: 'EPSG:4326', tileSize: 32, bandCount: 1, maxZoom: 2,
      loader: () => new Float32Array(32 * 32).fill(42)
    })
    const tiles = []
    try {
      for (const [z, x, y] of [[1, 0, 0], [1, 1, 0], [1, 0, 1], [1, 1, 1]]) {
        const tile = source.getTile(z, x, y, 1, getProjection('EPSG:3857'))
        await new Promise((resolve, reject) => {
          const changed = () => {
            if (tile.getState() === 2) { tile.removeEventListener('change', changed); resolve() }
            if (tile.getState() === 3) { tile.removeEventListener('change', changed); reject(tile.getError()) }
          }
          tile.addEventListener('change', changed)
          tile.load()
          changed()
        })
        const data = tile.getData()
        tiles.push({ length: data.length, hasValue: data.some(value => value === 42) })
      }
    } finally {
      source.dispose()
      CanvasRenderingContext2D.prototype.getImageData = original
    }
    return { tiles, reads }
  })
  expect(result.tiles).toHaveLength(4)
  expect(result.tiles.every(tile => tile.length > 0 && tile.hasValue)).toBe(true)
  expect(diagnostics, JSON.stringify(result.reads)).toEqual([])
})
