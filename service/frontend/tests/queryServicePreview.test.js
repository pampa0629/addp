import assert from 'node:assert/strict'
import test from 'node:test'
import { readFile } from 'node:fs/promises'
import vm from 'node:vm'

import {
  buildQueryServicePreview,
  queryServicePreviewFields
} from '../src/utils/queryServicePreview.js'

test('a new preview clears previous rows and cursor metadata even when it fails or returns no array', async () => {
  const source = await readFile(new URL('../src/views/QueryServiceDetail.vue', import.meta.url), 'utf8')
  const method = source.slice(source.indexOf('const loadPreviewData = async'), source.indexOf('const handlePreviewPageChange'))
  for (const outcome of ['empty', 'invalid', 'error']) {
    let settle
    const pending = new Promise((resolve, reject) => {
      settle = () => outcome === 'error' ? reject(new Error('query failed')) : resolve({ data: outcome === 'empty' ? [] : null })
    })
    const state = {
      service: { value: { service_name: 'metric', config_type: 'sql', named_parameters: [] } },
      previewData: { value: [{ subject_id: 'previous-person', value: 224 }] },
      previewLoading: { value: false },
      previewPagination: { value: { page: 1, pageSize: 20, cursors: [''], hasMore: true, nextCursor: 'old-cursor' } },
      queryUnavailable: { value: false }, metricBindingRequired: { value: false },
      previewNamedParameterValues: {}, defaultFields: { value: null }, spatialInfo: { value: null },
      queryServicePreviewFields: () => [], queryServiceAPI: { testQuery: () => pending },
      ElMessage: { warning() {}, success() {}, error() {} }, t: key => key, console: { error() {} }
    }
    const context = vm.createContext(state)
    const load = vm.runInContext(`${method}\nloadPreviewData`, context)
    const result = load()
    assert.equal(state.previewData.value.length, 0, `${outcome}: old rows remain during request`)
    assert.equal(state.previewPagination.value.hasMore, false)
    assert.equal(state.previewPagination.value.nextCursor, '')
    settle()
    await result
    assert.equal(state.previewData.value.length, 0, `${outcome}: old rows reappeared`)
    assert.equal(state.previewLoading.value, false)
  }
})

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
