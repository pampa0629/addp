import assert from 'node:assert/strict'
import test from 'node:test'
import { buildGeoJSONFeatures, spatialPreviewDescriptor, validateGeoJSONResult } from '../src/utils/geoJSONResult.mjs'

test('builds GeoJSON features from the descriptor-selected geometry field', () => {
  const rows = [{ shape: { type: 'Point', coordinates: [1, 2] }, name: 'A', hidden: 'x' }]
  const features = buildGeoJSONFeatures(rows, { geometry_field: 'shape', tooltip_fields: ['name'] })
  assert.deepEqual(features[0].geometry.coordinates, [1, 2])
  assert.deepEqual(features[0].properties, { name: 'A' })
  assert.equal(validateGeoJSONResult(rows, true).reason, 'partial_result')
})

test('uses explicit spatial contract facts without guessing a field name', () => {
  const preview = spatialPreviewDescriptor({
    srid: 3857,
    geometry_fields: [{ name: 'shape', srid: 4326, crs_ref: 'EPSG:4326' }]
  }, 'shape')
  assert.deepEqual(preview, { source_srid: 4326, source_crs: 'EPSG:4326' })
})
