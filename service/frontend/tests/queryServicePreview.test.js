import assert from 'node:assert/strict'
import test from 'node:test'
import { readFile } from 'node:fs/promises'
import vm from 'node:vm'

import {
  buildQueryServicePreview,
  queryServicePreviewFields, buildQueryServicePreviewRequest
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
      previewSequence: 0,
      service: { value: { service_name: 'metric', config_type: 'sql', named_parameters: [] } },
      previewError: { value: 'previous failure' },
      previewData: { value: [{ subject_id: 'previous-person', value: 224 }] },
      previewLoading: { value: false },
      previewPagination: { value: { page: 1, pageSize: 20, cursors: [''], hasMore: true, nextCursor: 'old-cursor' } },
      queryUnavailable: { value: false }, metricBindingRequired: { value: false },
      previewNamedParameterValues: {}, defaultFields: { value: null }, spatialInfo: { value: null },
      buildPreviewRequest: () => ({ parameters: {}, page: { limit: 20, cursor: '' }, format: 'json' }), queryServiceAPI: { testQuery: () => pending },
      ElMessage: { warning() {}, success() {}, error() {} }, t: key => key, console: { error() {} }
    }
    const context = vm.createContext(state)
    const load = vm.runInContext(`${method}\nloadPreviewData`, context)
    const result = load()
    assert.equal(state.previewError.value, '', `${outcome}: old failure remains during request`)
    assert.equal(state.previewData.value.length, 0, `${outcome}: old rows remain during request`)
    assert.equal(state.previewPagination.value.hasMore, false)
    assert.equal(state.previewPagination.value.nextCursor, '')
    settle()
    await result
    assert.equal(state.previewData.value.length, 0, `${outcome}: old rows reappeared`)
    assert.equal(state.previewLoading.value, false)
  }
})

test('a handled protection refusal stays visible without a runtime console error', async () => {
  const source = await readFile(new URL('../src/views/QueryServiceDetail.vue', import.meta.url), 'utf8')
  const method = source.slice(source.indexOf('const loadPreviewData = async'), source.indexOf('const handlePreviewPageChange'))
  for (const [status, code, expectedLogs] of [
    [403, 'data_protection_required', 0], [403, 'query_protection_roles_unavailable', 0],
    [409, 'query_protection_choice_invalid', 0], [403, 'query_protection_choice_invalid', 1], [403, 'permission_denied', 1],
    [500, 'data_protection_required', 1], [undefined, undefined, 1]
  ]) {
    const logs = []
    const messages = []
    const error = { message: 'request failed', response: status ? { status, data: { error_code: code, error: 'controlled reason' } } : undefined }
    const state = {
      previewSequence: 0,
      service: { value: { service_name: 'protected', named_parameters: [] } },
      previewError: { value: 'previous failure' },
      previewData: { value: [{ id: 'old-result' }] }, previewLoading: { value: false },
      previewPagination: { value: { hasMore: true, nextCursor: 'old-cursor' } },
      queryUnavailable: { value: false }, previewNamedParameterValues: {},
      buildPreviewRequest: () => ({}), queryServiceAPI: { testQuery: async () => { throw error } },
      ElMessage: { error: message => messages.push(message) }, t: key => key,
      console: { error: (...args) => logs.push(args) }
    }
    const load = vm.runInContext(`${method}\nloadPreviewData`, vm.createContext(state))
    await load()
    assert.equal(logs.length, expectedLogs, `${status}/${code}`)
    const visibleError = `service.query.loadPreviewFailed: ${error.response?.data.error || error.message}`
    assert.equal(state.previewError.value, visibleError)
    assert.deepEqual(messages, expectedLogs ? [visibleError] : [])
    assert.equal(state.previewPagination.value.page, 1)
    assert.deepEqual(Array.from(state.previewPagination.value.cursors), [''])
    assert.equal(state.previewData.value.length, 0)
    assert.equal(state.previewLoading.value, false)
    assert.equal(state.previewPagination.value.hasMore, false)
    assert.equal(state.previewPagination.value.nextCursor, '')
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


test('inspection and data preview preserve typed values, field selection and the current page cursor', () => {
  const request = buildQueryServicePreviewRequest({ parameters: [{name:'person'}, {name:'zero'}, {name:'flag'}, {name:'empty'}],
    values: {person:'9007199254740993', zero:0, flag:false, empty:'', extra:'excluded'},
    pagination:{page:2, pageSize:50, cursors:['','opaque']}, defaultFields:['value','value'] })
  assert.deepEqual(request, {parameters:{person:'9007199254740993',zero:0,flag:false}, page:{limit:50,cursor:'opaque'},format:'json',select:['value']})
})

test('query inspection clears old results and discards in-flight responses after conditions change', async () => {
  const source = await readFile(new URL('../src/views/QueryServiceDetail.vue', import.meta.url), 'utf8')
  const method = source.slice(source.indexOf('const executionQuery = ref'), source.indexOf('// 数据预览方法'))
  let invalidate, resolve
  const state = { ref: value => ({value}), watch: (_getter, callback) => { invalidate = callback },
    serviceId:{value:35}, service:{value:{version:9,named_parameters:[]}}, previewNamedParameterValues:{},
    buildPreviewRequest: () => ({parameters:{subject_id:'A'}}),
    queryServiceAPI:{previewExecutionQuery: () => new Promise(done => {resolve=done})}, t:key=>key }
  const api = vm.runInNewContext(`${method}; ({loadExecutionQuery, executionQuery, executionQueryLoading, executionQueryError})`,state)
  const pending = api.loadExecutionQuery()
  assert.equal(api.executionQueryLoading.value,true)
  invalidate()
  resolve({version:9,query:'stale'})
  await pending
  assert.equal(api.executionQuery.value,null)
  assert.equal(api.executionQueryLoading.value,false)
  const second = api.loadExecutionQuery()
  resolve({version:10,query:'new definition'})
  await second
  assert.equal(api.executionQuery.value,null)
  assert.equal(api.executionQueryError.value,'service.query.versionConflict')
})


async function pendingPreview() {
  const source = await readFile(new URL('../src/views/QueryServiceDetail.vue', import.meta.url), 'utf8')
  const method = source.slice(source.indexOf('const loadPreviewData = async'), source.indexOf('const handlePreviewPageChange'))
  const pending = []
  const notices = []
  const state = {
    previewSequence: 0,
    service: { value: { service_name: 'protected', version: 1, named_parameters: [] } },
    previewError: { value: '' }, previewData: { value: [] }, previewLoading: { value: false },
    previewPagination: { value: { page: 1, pageSize: 20, cursors: [''], hasMore: false, nextCursor: '' } },
    queryUnavailable: { value: false }, previewNamedParameterValues: {}, buildPreviewRequest: () => ({}),
    queryServiceAPI: { testQuery: () => new Promise((resolve, reject) => pending.push({ resolve, reject })) },
    ElMessage: { success: text => notices.push(text), error: text => notices.push(text), warning: text => notices.push(text) },
    t: key => key, console: { error() {} }
  }
  return { state, pending, notices, load: vm.runInContext(`${method}\nloadPreviewData`, vm.createContext(state)) }
}
const protectedPreview = { data: [{ id: '1', phone: '138****0001' }], page: { limit: 20, has_more: true, next_cursor: 'obsolete' } }
const protectionRefusal = { response: { status: 403, data: { error_code: 'data_protection_required', error: 'expired protection' } } }

test('late success or error cannot overwrite a newer protection refusal or successful recovery', async () => {
  for (const older of ['success', 'error']) {
    for (const current of ['refusal', 'recovery']) {
      const { state, pending, notices, load } = await pendingPreview()
      const first = load()
      const second = load()
      if (current === 'refusal') pending[1].reject(protectionRefusal)
      else pending[1].resolve({ data: [{ id: 'new', phone: '***********' }], page: { limit: 20, has_more: false } })
      await second
      const before = JSON.stringify([state.previewData.value, state.previewError.value, state.previewPagination.value, notices])
      if (older === 'success') pending[0].resolve(protectedPreview)
      else pending[0].reject(protectionRefusal)
      await first
      assert.equal(JSON.stringify([state.previewData.value, state.previewError.value, state.previewPagination.value, notices]), before, `${older}/${current}`)
      assert.equal(state.previewLoading.value, false)
    }
  }
})

test('an older preview settling cannot release a newer request loading state', async () => {
  const { state, pending, load } = await pendingPreview()
  const first = load()
  const second = load()
  pending[0].resolve(protectedPreview)
  await first
  assert.equal(state.previewLoading.value, true)
  assert.equal(state.previewData.value.length, 0)
  pending[1].reject(protectionRefusal)
  await second
  assert.equal(state.previewLoading.value, false)
})

test('changed or inactive service definitions discard pending preview rows and notices', async () => {
  for (const inactive of [false, true]) {
    const { state, pending, notices, load } = await pendingPreview()
    const result = load()
    state.service.value = { ...state.service.value, version: 2, status: inactive ? 'inactive' : 'active' }
    state.queryUnavailable.value = inactive
    pending[0].resolve(protectedPreview)
    await result
    assert.equal(state.previewData.value.length, 0)
    assert.equal(state.previewPagination.value.nextCursor, '')
    assert.equal(state.previewError.value, '')
    assert.equal(state.previewLoading.value, false)
    assert.deepEqual(notices, [])
  }
})
