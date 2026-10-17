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

for (const exposeStatus of [false, true]) {
  test(`PMTiles cross-origin tile headers ${exposeStatus ? 'expose decoded render status' : 'remain hidden without CORS exposure'}`, async ({ page }) => {
    const { createServer } = await import('node:http')
    const tile = Buffer.from([26, 24, 10, 6, 112, 111, 105, 110, 116, 115, 18, 9, 24, 1, 34, 5, 9, 194, 43, 176, 11, 40, 128, 32, 120, 2])
    const requests = []
    const server = createServer((request, response) => {
      response.setHeader('Access-Control-Allow-Origin', request.headers.origin || '*')
      response.setHeader('Access-Control-Allow-Headers', 'Authorization')
      response.setHeader('Access-Control-Allow-Methods', 'GET, OPTIONS')
      if (request.method === 'OPTIONS') { response.writeHead(204); response.end(); return }
      const pathname = new URL(request.url, 'http://tile.invalid').pathname
      const match = pathname.match(/\/tiles\/(\d+)\/(\d+)\/(\d+)\.mvt$/)
      if (!match) { response.writeHead(404); response.end(); return }
      requests.push(match.slice(1).map(Number))
      const body = match.slice(1).join('/') === '5/26/13' ? tile : Buffer.alloc(0)
      response.setHeader('Content-Type', 'application/vnd.mapbox-vector-tile')
      response.setHeader('Cache-Control', 'no-store')
      response.setHeader('X-ADDP-Render-Source', 'business_pmtiles')
      response.setHeader('X-ADDP-Tile-Cache', 'MISS')
      response.setHeader('X-ADDP-Tile-Status', body.length ? 'ok' : 'empty')
      if (exposeStatus) response.setHeader('Access-Control-Expose-Headers', 'X-ADDP-Render-Source, X-ADDP-Tile-Cache, X-ADDP-Tile-Status')
      response.end(body)
    })
    await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
    const origin = `http://127.0.0.1:${server.address().port}`
    try {
      const locator = 'addp://engine/2/path/low-zoom.pmtiles?type=file&item_id=8'
      const rootLocator = 'addp://engine/2/path/?type=server&node_id=1'
      const item = { id: locator, locator, label: 'low-zoom.pmtiles', type: 'file', hasChildren: false }
      const root = { id: rootLocator, locator: rootLocator, label: 'Tile source', type: 'server', hasChildren: true, children: [item] }
      await page.addInitScript(() => {
        localStorage.setItem('addp-lang', 'zh-cn')
        window.pmtilesReadableHeaders = []
        const nativeFetch = window.fetch
        window.fetch = async (input, options) => {
          const response = await nativeFetch(input, options)
          if (String(input).includes('/quick-view/tiles/')) window.pmtilesReadableHeaders.push({
            cache: options?.cache, source: response.headers.get('X-ADDP-Render-Source'), status: response.headers.get('X-ADDP-Tile-Cache')
          })
          return response
        }
      })
      await page.route('**/api/v1/**', async route => {
        const url = new URL(route.request().url()), path = url.pathname
        if (url.origin === origin) return route.continue()
        const json = body => route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) })
        if (path.endsWith('/system/refresh')) return json({ access_token: 'pmtiles-e2e-token', expires_in: 3600 })
        if (path.endsWith('/system/users/me')) return json({ id: '1', display_name: 'pmtiles-e2e', local_account: { username: 'pmtiles-e2e' } })
        if (path.endsWith('/system/auth/context')) return json(managerAuthContext)
        if (path.endsWith('/manager/engines')) return json({ data: [{ id: 2, name: root.label, engine_type: 'nfs', lifecycle_state: 'active', connection_status: 'online' }] })
        if (path.endsWith('/ancestors')) return json({ target_locator: locator, ancestors: [{ ...root, children: [] }, item] })
        if (path.endsWith('/meta/resource-tree/2') || path.endsWith('/meta/resource-tree/2/node')) return json(root)
        if (path.endsWith('/manager/preview')) return json({ preview_type: 'object', data: { mode: 'object', object: { path: item.label, content: {
          kind: 'vector_tile', frontend_renderer: 'vector_tile', preview_material: 'url',
          url: `${origin}/api/v1/manager/quick-view/tiles/{z}/{x}/{y}.mvt?locator=${encodeURIComponent(locator)}`,
          metadata: { locator, render_source: 'business_pmtiles', min_zoom: 0, max_zoom: 5, extent: [119.99, 30.07, 120.3, 30.4], extent_srid: 4326 }
        } } } })
        return json({})
      })
      await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}`)
      await expect.poll(() => requests.length).toBeGreaterThan(0)
      expect(requests.every(([zoom]) => zoom <= 5)).toBe(true)
      const badge = page.locator('.vector-tile-preview .render-status-badge')
      await page.waitForFunction(() => window.pmtilesReadableHeaders?.length > 0)
      await expect(badge).toHaveClass(exposeStatus ? /is-dynamic/ : /is-unknown/)
      await badge.hover()
      await expect(page.getByRole('tooltip').filter({ hasText: '当前渲染轮次要素数：1' })).toBeVisible()
      const headers = await page.evaluate(() => window.pmtilesReadableHeaders)
      expect(headers.length).toBeGreaterThan(0)
      expect(headers.every(value => value.cache === 'no-store')).toBe(true)
      expect(headers.every(value => value.source === (exposeStatus ? 'business_pmtiles' : null) && value.status === (exposeStatus ? 'MISS' : null))).toBe(true)
      const canvas = page.locator('.vector-tile-preview .ol-viewport canvas')
      await expect.poll(() => canvas.evaluate(element => {
        const pixels = element.getContext('2d').getImageData(0, 0, element.width, element.height).data
        let count = 0
        for (let offset = 3; offset < pixels.length; offset += 4) if (pixels[offset]) count++
        return count
      })).toBeGreaterThan(30)
    } finally {
      server.closeAllConnections()
      await new Promise(resolve => server.close(resolve))
    }
  })
}
