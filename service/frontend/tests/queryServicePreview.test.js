import assert from 'node:assert/strict'
import test from 'node:test'

import {
  buildQueryServicePreview,
  queryServicePreviewFields
} from '../src/utils/queryServicePreview.js'

test('uses published geometry metadata for map preview', () => {
  const rows = [{ id: 1, custom_shape: '{"type":"Point","coordinates":[120,30]}' }]
  const result = buildQueryServicePreview({
    rows,
    pagination: { page: 2, page_size: 20, total: 41 },
    spatial: {
      geometry_columns: [{ name: 'custom_shape', geometry_type: 'Point', srid: 4326, crs_ref: 'EPSG:4326' }],
      primary_geometry_column: 'custom_shape'
    }
  })

  assert.deepEqual(result.columns, ['id', 'custom_shape'])
  assert.deepEqual(result.rows, rows)
  assert.deepEqual(result.geometry_columns, ['custom_shape'])
  assert.equal(result.source_srid, 4326)
  assert.equal(result.source_crs, 'EPSG:4326')
  assert.equal(result.transform_status, 'not_transformed')
  assert.equal(result.page, 2)
  assert.equal(result.page_size, 20)
	assert.equal(result.total, 21)
})

test('does not infer geometry columns from field names', () => {
  const result = buildQueryServicePreview({
    rows: [{ id: 1, geometry: 'not published as spatial data' }],
    pagination: { page: 1, page_size: 10, total: 1 },
    spatial: null
  })

  assert.deepEqual(result.geometry_columns, [])
  assert.equal(result.transform_status, 'unknown_crs')
})

test('marks spatial data without a known SRID as unsafe to render', () => {
  const result = buildQueryServicePreview({
    rows: [{ shape: '{"type":"Point","coordinates":[0,0]}' }],
    spatial: {
      geometry_columns: [{ name: 'shape', geometry_type: 'Point' }],
      primary_geometry_column: 'shape'
    }
  })

  assert.deepEqual(result.geometry_columns, ['shape'])
  assert.equal(result.source_crs, '')
  assert.equal(result.transform_status, 'unknown_crs')
})

test('requests the published geometry column in table preview', () => {
  const fields = queryServicePreviewFields({
    configType: 'table',
    defaultFields: ['id', 'name'],
    spatial: {
      geometry_columns: [{ name: 'custom_shape' }],
      primary_geometry_column: 'custom_shape'
    }
  })

	assert.deepEqual(fields, ['id', 'name', 'custom_shape'])
})

test('does not duplicate geometry or constrain SQL query fields', () => {
	assert.deepEqual(queryServicePreviewFields({
    configType: 'table',
    defaultFields: ['id', 'custom_shape'],
    spatial: { geometry_columns: [{ name: 'custom_shape' }], primary_geometry_column: 'custom_shape' }
	}), ['id', 'custom_shape'])
	assert.deepEqual(queryServicePreviewFields({
    configType: 'sql',
    defaultFields: ['id'],
    spatial: { geometry_columns: [{ name: 'custom_shape' }], primary_geometry_column: 'custom_shape' }
	}), ['id', 'custom_shape'])
	assert.deepEqual(queryServicePreviewFields({
    configType: 'table',
    defaultFields: null,
    spatial: { geometry_columns: [{ name: 'custom_shape' }], primary_geometry_column: 'custom_shape' }
	}), [])
})

test('forwards an arbitrary CRS definition from the published snapshot', () => {
  const definition = {
    id: 'EPSG:32650',
    definition_encoding: 'wkt',
    definition: 'PROJCS["WGS 84 / UTM zone 50N",...]',
    source: 'postgis_spatial_ref_sys'
  }
  const result = buildQueryServicePreview({
    rows: [{ shape: '{"type":"Point","coordinates":[500000,3500000]}' }],
    spatial: {
      geometry_columns: [{ name: 'shape', srid: 32650, crs_ref: 'EPSG:32650' }],
      primary_geometry_column: 'shape',
      crs_definitions: [definition]
    }
  })

  assert.equal(result.source_crs, 'EPSG:32650')
  assert.deepEqual(result.source_crs_definition, definition)
  assert.equal(result.transform_status, 'not_transformed')
})
