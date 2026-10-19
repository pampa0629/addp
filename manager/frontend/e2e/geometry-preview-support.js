import WKB from 'ol/format/WKB.js'
import GeoJSON from 'ol/format/GeoJSON.js'

const wkb = new WKB()
const geojson = new GeoJSON()

// The scope-table HTTP contract carries []byte as canonical Base64 EWKB.
// No projection options: expected fixtures assert the original source axes.
export function decodePreviewEWKB(value) {
  if (typeof value !== 'string' || !value || value.length % 4 !== 0 || !/^[A-Za-z0-9+/]+={0,2}$/.test(value)) {
    throw new Error('Expected a Base64 EWKB geometry cell')
  }
  const bytes = Buffer.from(value, 'base64')
  if (bytes.toString('base64') !== value) throw new Error('Noncanonical Base64 EWKB geometry cell')
  const geometry = wkb.readGeometry(new Uint8Array(bytes).buffer)
  if (!geometry) throw new Error('Empty EWKB geometry cell')
  return geojson.writeGeometryObject(geometry)
}

// Independent spherical-Mercator oracle for the preview's geographic handoff
// followed by the map projection. Source EWKB still uses exact original axes.
// Do not round these doubles or replace the comparison with an epsilon.
export function webMercatorPreviewCoordinate([x, y]) {
  const radius = 6378137
  const longitude = x / radius * (180 / Math.PI)
  const latitude = (Math.PI / 2 - 2 * Math.atan(Math.exp(-y / radius))) * (180 / Math.PI)
  return [Math.PI * radius * longitude / 180,
    radius * Math.log(Math.tan(Math.PI * (latitude + 90) / 360))]
}

// OpenLayers' default accessible name is the minus glyph; its tooltip is
// not the button's accessible name. Use the control's stable DOM class.
export async function zoomOutPreviewMap(scope, expect) {
  const renderer = scope.locator('.openlayers-map-renderer')
  const button = renderer.locator('button.ol-zoom-out')
  await expect(button).toBeVisible()
  const zoom = () => renderer.evaluate(element => {
    const value = element.__vueParentComponent?.setupState?.mapInstance
    const map = value?.value || value
    const view = map?.getView()
    return view && !view.getAnimating() ? view.getZoom() : null
  })
  await expect.poll(async () => Number.isFinite(await zoom())).toBe(true)
  for (let step = 0; step < 2; step++) {
    const before = await zoom()
    expect(Number.isFinite(before)).toBe(true)
    await button.click()
    await expect.poll(zoom).toBeCloseTo(before - 1, 6)
  }
}
