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
