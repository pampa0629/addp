import { expect, test } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'

test('PMTiles starting at zoom zero renders when its maximum zoom is below six', async ({ page }) => {
  const locator = 'addp://engine/2/path/low-zoom.pmtiles?type=file&item_id=8'
  const rootLocator = 'addp://engine/2/path/?type=server&node_id=1'
  const item = { id: locator, locator, label: 'low-zoom.pmtiles', type: 'file', hasChildren: false }
  const root = { id: rootLocator, locator: rootLocator, label: 'Tile source', type: 'server', hasChildren: true, children: [item] }
  const tiles = [], errors = []
  // MVT v2: a point near Hangzhou in z5/x26/y13, without attributes.
  const tile = Buffer.from([26, 24, 10, 6, 112, 111, 105, 110, 116, 115, 18, 9, 24, 1, 34, 5, 9, 194, 43, 176, 11, 40, 128, 32, 120, 2])
  page.on('pageerror', error => errors.push(error.message))
  await page.addInitScript(() => {
    localStorage.setItem('addp-lang', 'zh-cn')
    window.pmtilesRequestCaches = []
    const nativeFetch = window.fetch
    window.fetch = (input, options) => {
      if (String(input).includes('/quick-view/tiles/')) window.pmtilesRequestCaches.push(options?.cache)
      return nativeFetch(input, options)
    }
  })
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url()), path = url.pathname
    const json = value => route.fulfill({ contentType: 'application/json', body: JSON.stringify(value) })
    if (path.endsWith('/system/refresh')) return json({ access_token: 'pmtiles-e2e-token', expires_in: 3600 })
    if (path.endsWith('/system/users/me')) return json({ id: '1', display_name: 'pmtiles-e2e', local_account: { username: 'pmtiles-e2e' } })
    if (path.endsWith('/system/auth/context')) return json(managerAuthContext)
    if (path.endsWith('/manager/engines')) return json({ data: [{ id: 2, name: root.label, engine_type: 'nfs', lifecycle_state: 'active', connection_status: 'online' }] })
    if (path.endsWith('/ancestors')) return json({ target_locator: locator, ancestors: [{ ...root, children: [] }, item] })
    if (path.endsWith('/meta/resource-tree/2') || path.endsWith('/meta/resource-tree/2/node')) return json(root)
    if (path.endsWith('/manager/preview')) return json({ preview_type: 'object', data: {
      mode: 'object', object: { path: item.label, content: {
        kind: 'vector_tile', frontend_renderer: 'vector_tile', preview_material: 'url',
        url: `/api/v1/manager/quick-view/tiles/{z}/{x}/{y}.mvt?locator=${encodeURIComponent(locator)}`,
        metadata: { locator, render_source: 'business_pmtiles', min_zoom: 0, max_zoom: 5, extent: [119.99, 30.07, 120.3, 30.4], extent_srid: 4326 }
      } }
    } })
    const match = path.match(/\/quick-view\/tiles\/(\d+)\/(\d+)\/(\d+)\.mvt$/)
    if (match) {
      tiles.push({ zoom: Number(match[1]), locator: url.searchParams.get('locator') })
      return route.fulfill({ contentType: 'application/vnd.mapbox-vector-tile', body: match.slice(1).join('/') === '5/26/13' ? tile : Buffer.alloc(0) })
    }
    return json({})
  })
  await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}`)
  await expect.poll(() => tiles.length).toBeGreaterThan(0)
  expect(tiles.every(request => request.zoom <= 5 && request.locator === locator)).toBe(true)
  const requestCaches = await page.evaluate(() => window.pmtilesRequestCaches)
  expect(requestCaches.length).toBeGreaterThan(0)
  expect(requestCaches.every(cache => cache === 'no-store')).toBe(true)
  const canvas = page.locator('.vector-tile-preview .ol-viewport canvas')
  await expect(canvas).toBeVisible()
  await expect.poll(() => canvas.evaluate(element => {
    const pixels = element.getContext('2d').getImageData(0, 0, element.width, element.height).data
    let painted = 0
    for (let i = 3; i < pixels.length; i += 4) if (pixels[i] > 0) painted++
    return painted
  })).toBeGreaterThan(30)
  expect(errors).toEqual([])
})
