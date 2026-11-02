import { expect, test } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'

const transparentPNG = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==', 'base64')
const styleModuleURL = `/@fs${new URL('../../../common-frontend/map/src/utils/mapStyles.js', import.meta.url).pathname}`
const business = { id: 0, geometry: '业务<geometry>&', geom: '普通geom', rowData: '业务rowData', originalFeature: '业务originalFeature', __addpGeometry: '业务内部同名' }
const shapes = {
  Point: { type: 'Point', coordinates: [10, 20] },
  LineString: { type: 'LineString', coordinates: [[10, 20], [11, 21]] },
  Polygon: { type: 'Polygon', coordinates: [[[10, 20], [11, 20], [11, 21], [10, 20]]] },
  GeometryCollection: { type: 'GeometryCollection', geometries: [
    { type: 'GeometryCollection', geometries: [{ type: 'Point', coordinates: [10, 20] }] },
    { type: 'LineString', coordinates: [[10.2, 20.2], [10.4, 20.2]] },
    { type: 'Polygon', coordinates: [[[10.5, 20.5], [10.8, 20.5], [10.8, 20.8], [10.5, 20.5]]] },
  ] },
}

for (const [type, shape] of Object.entries(shapes)) {
  test(`shared map preserves business names through ${type} render, style, popup and highlight`, async ({ page }) => {
    const errors = []
    page.on('pageerror', error => errors.push(error.message))
    const locator = 'addp://engine/2/path/properties.gml?type=file&item_id=8'
    const rootLocator = 'addp://engine/2/path/?type=server&node_id=1'
    const item = { id: locator, locator, label: 'properties.gml', type: 'file', hasChildren: false }
    const root = { id: rootLocator, locator: rootLocator, label: 'Map properties', type: 'server', hasChildren: true, children: [item] }
    await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
    await page.route('**/*openstreetmap.org/**', route => route.fulfill({ contentType: 'image/png', headers: { 'access-control-allow-origin': '*' }, body: transparentPNG }))
    await page.route('**/plugins/manifest.json', route => json(route, { scripts: ['/plugins/table-preview.js'] }))
    await page.route('**/api/v1/**', route => {
      const path = new URL(route.request().url()).pathname
      if (path.endsWith('/system/refresh')) return json(route, { access_token: 'map-property-e2e', expires_in: 3600 })
      if (path.endsWith('/system/users/me')) return json(route, { id: '1', display_name: 'map-property-e2e', local_account: { username: 'map-property-e2e' } })
      if (path.endsWith('/system/auth/context')) return json(route, managerAuthContext)
      if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 2, name: root.label, engine_type: 'nfs', lifecycle_state: 'active', connection_status: 'online' }] })
      if (path.endsWith('/ancestors')) return json(route, { target_locator: locator, ancestors: [{ ...root, children: [] }, item] })
      if (path.endsWith('/meta/resource-tree/2') || path.endsWith('/meta/resource-tree/2/node')) return json(route, root)
      if (path.endsWith('/manager/preview')) return json(route, { preview_type: 'table', data: {
        mode: 'table', columns: [...Object.keys(business), 'source_shape'], geometry_columns: ['source_shape'], geometry_column: 'source_shape',
        source_srid: 4326, source_crs: 'EPSG:4326', transform_status: 'not_transformed',
        rows: [{ ...business, source_shape: shape }], page: 1, page_size: 20, total: 1,
      } })
      return json(route, {})
    })
    await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}`)
    const renderer = page.locator('.table-preview .openlayers-map-renderer')
    await expect(renderer.locator('canvas').first()).toBeVisible()
    await expect.poll(() => renderer.evaluate(element => {
      const value = element.__vueParentComponent.setupState.mapInstance
      const map = value?.value || value
      return map.getLayers().getArray().flatMap(layer => layer.getSource?.()?.getFeatures?.() || []).length
    })).toBe(1)
    const observed = await renderer.evaluate(async (element, { business }) => {
      const value = element.__vueParentComponent.setupState.mapInstance
      const map = value?.value || value
      const layers = map.getLayers().getArray().filter(layer => layer.getSource?.()?.getFeatures)
      const feature = layers.flatMap(layer => layer.getSource().getFeatures())[0]
      const geometry = feature.getGeometry()
      const coordinate = geometry.getClosestPoint(map.getView().getCenter())
      const pixel = map.getPixelFromCoordinate(coordinate)
      return { fields: Object.fromEntries(Object.keys(business).map(key => [key, feature.get(key)])), type: geometry.getType(), pixel,
        extent: geometry.getExtent(), geometryName: feature.getGeometryName() }
    }, { business })
    expect(observed.fields).toEqual(business)
    expect(observed.type).toBe(type)
    expect(observed.geometryName).not.toBe('geometry')
    expect(observed.extent.every(Number.isFinite)).toBe(true)
    if (type === 'GeometryCollection') {
      await expect.poll(() => collectionPointPixels(renderer)).toBeGreaterThan(10)
      // Also exercise the tile default factory on the real OpenLayers feature.
      await renderer.evaluate(async (element, styleModuleURL) => {
        const { createDefaultStyleFunction } = await import(styleModuleURL)
        const value = element.__vueParentComponent.setupState.mapInstance
        const map = value?.value || value
        map.getLayers().getArray().find(layer => layer.getSource?.()?.getFeatures?.().length).setStyle(createDefaultStyleFunction())
        map.renderSync()
      }, styleModuleURL)
      await expect.poll(() => collectionPointPixels(renderer)).toBeGreaterThan(10)
    }
    // Use the production thematic factory on the real rendered feature and browser theme.
    const width = await renderer.evaluate(async (element, styleModuleURL) => {
      const { createThematicFeatureStyle } = await import(styleModuleURL)
      const value = element.__vueParentComponent.setupState.mapInstance
      const map = value?.value || value
      const layer = map.getLayers().getArray().find(layer => layer.getSource?.()?.getFeatures?.().length)
      const feature = layer.getSource().getFeatures()[0]
      const style = createThematicFeatureStyle(feature, {
        valid: true, mode: 'categorical', field: 'geometry', categories: ['other', '业务<geometry>&', 'third'], entries: [1, 2, 3], palette: 'primary',
      })
      layer.setStyle(style)
      map.renderSync()
      return style.getImage?.()?.getRadius() || style.getStroke?.()?.getWidth()
    }, styleModuleURL)
    expect(width).toBe(['Point', 'GeometryCollection'].includes(type) ? 6 : (type === 'LineString' ? 3 : 1.5))
    if (type === 'GeometryCollection') await expect.poll(() => collectionPointPixels(renderer)).toBeGreaterThan(10)
    // Real table selection highlights; a real pointer map click opens the popup.
    await page.getByRole('cell', { name: business.geometry, exact: true }).click()
    await expect.poll(() => renderer.evaluate(element => {
      const value = element.__vueParentComponent.setupState.mapInstance
      return !(value?.value || value).getView().getAnimating()
    })).toBe(true)
    const pixel = await renderer.evaluate(element => {
      const value = element.__vueParentComponent.setupState.mapInstance
      const map = value?.value || value
      const feature = map.getLayers().getArray().flatMap(layer => layer.getSource?.()?.getFeatures?.() || [])[0]
      return map.getPixelFromCoordinate(feature.getGeometry().getClosestPoint(map.getView().getCenter()))
    })
    await renderer.click({ position: { x: pixel[0], y: pixel[1] } })
    const popup = page.locator('.table-preview .map-popup:visible').first()
    await expect(popup).toContainText(business.geometry)
    await expect(popup).toContainText(business.geom)
    await expect(popup).toContainText(business.rowData)
    await expect(popup).toContainText(business.originalFeature)
    await expect.poll(() => renderer.evaluate(element => {
      const value = element.__vueParentComponent.setupState.mapInstance
      const map = value?.value || value
      return map.getLayers().getArray().flatMap(layer => layer.getSource?.()?.getFeatures?.() || []).length
    })).toBe(2)
    expect(errors).toEqual([])
  })
}
function json(route, body) {
  return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
}

async function collectionPointPixels(renderer) {
  return renderer.evaluate(element => {
    const value = element.__vueParentComponent.setupState.mapInstance
    const map = value?.value || value
    if (!map || map.getView().getAnimating()) return 0
    const layer = map.getLayers().getArray().find(layer => layer.getSource?.()?.getFeatures?.().length)
    const feature = layer?.getSource().getFeatures()[0]
    const pending = [feature?.getGeometry()]
    let point
    while (pending.length) {
      const geometry = pending.pop()
      if (geometry?.getType() === 'Point') { point = geometry; break }
      if (geometry?.getType() === 'GeometryCollection') pending.push(...geometry.getGeometries())
    }
    const context = layer?.getRenderer()?.context
    if (!point || !context) return 0
    const pixel = map.getPixelFromCoordinate(point.getCoordinates())
    // A canvas can share a painted background. Require the collection's point
    // member itself to participate in the vector renderer's hit detection.
    const hit = map.forEachFeatureAtPixel(pixel, candidate => candidate === feature ? true : undefined,
      { layerFilter: candidate => candidate === layer, hitTolerance: 0 })
    if (hit !== true) return 0
    const rect = context.canvas.getBoundingClientRect(), viewport = element.getBoundingClientRect()
    const x = Math.round((viewport.left + pixel[0] - rect.left) * context.canvas.width / rect.width)
    const y = Math.round((viewport.top + pixel[1] - rect.top) * context.canvas.height / rect.height)
    const data = context.getImageData(x - 6, y - 6, 13, 13).data
    let count = 0
    for (let i = 3; i < data.length; i += 4) if (data[i] > 0) count++
    return count
  })
}
