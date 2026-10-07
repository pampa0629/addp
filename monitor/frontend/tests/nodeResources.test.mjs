import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { currentResourceValue, resourceChartRows, resolveResourceRoute, trendParameters, validateResourceResponse, filesystemMetrics, filesystemRows } from '../src/utils/nodeResources.js'

test('node resource trends delegate chart rendering and formatting to the shared owners', () => {
  const source = readFileSync(new URL('../src/views/NodeResources.vue', import.meta.url), 'utf8')
  assert.match(source, /common-frontend\/chart\/src\/ChartRenderer.vue/)
  assert.match(source, /common-frontend\/basic\/src\/utils\/fieldPresentation.mjs/)
  assert.doesNotMatch(source, /echarts|setOption|Intl\.NumberFormat|localStorage/)
})

test('restores only canonical list context and fixed catalog trend options', () => {
  const query = { page: '2', search: '节点', range: '7d', metric: 'node.load.average_1m' }
  assert.deepEqual(resolveResourceRoute(query, true).query, query)
  assert.equal(resolveResourceRoute(query, true).changed, false)
  const invalid = resolveResourceRoute({ page: '02', range: ['7d'], metric: 'arbitrary_query', token: 'secret' }, true)
  assert.deepEqual(invalid.query, {})
  assert.equal(invalid.metric, 'node.memory.used_percent')
  assert.deepEqual(resolveResourceRoute(query).query, { page: '2', search: '节点' })
})
test('anchors whole-second trend windows to the latest server evaluation, not browser time', () => {
  const params = trendParameters('node-id', 'node.memory.used_percent', '7d', '2026-10-07T00:00:00Z')
  assert.equal(params.start, '2026-09-30T00:00:00.000Z')
  assert.equal(params.end, '2026-10-07T00:00:00.000Z')
  assert.throws(() => trendParameters('node-id', '', '7d', 'bad-time'))
})
test('zero remains valid; stale and absent samples never become current values or chart zeros', () => {
  const points = ['valid', 'stale', 'no_data', 'not_connected'].map(data_state => ({ data_state, value: data_state === 'valid' ? 0 : 999, evaluated_at: '2026-10-07T00:00:00Z' }))
  assert.deepEqual(points.map(currentResourceValue), [0, null, null, null])
  assert.deepEqual(resourceChartRows({ points }).map(row => row.value), [0, null, null, null])
  assert.equal(currentResourceValue({ data_state: 'valid', value: null }), null)
})
test('rejects mismatched subjects, incomplete catalogs and invalid sample evidence', () => {
  const key = 'node.memory.used_percent', time = '2026-10-07T00:00:00Z'
  const response = { subject: { kind: 'node', node_id: 'node-id' }, end: time, queried_at: time, series: [{ metric_key: key, dimensions: {}, unit: 'percent', window_seconds: 0, points: [{ evaluated_at: time, sampled_at: time, value: 0, data_state: 'valid' }] }] }
  assert.equal(validateResourceResponse(response, 'node-id', [key]), response)
  assert.throws(() => validateResourceResponse(response, 'other-node', [key]))
  assert.throws(() => validateResourceResponse(response, 'node-id', [key, 'node.cpu.logical_cores']))
  for (const change of [{ value: null }, { sampled_at: null }, { data_state: 'unknown' }, { value: Infinity }]) {
    const value = structuredClone(response); Object.assign(value.series[0].points[0], change)
    assert.throws(() => validateResourceResponse(value, 'node-id', [key]))
  }
})


test('detail refresh has a single canonical bounded choice; list never retains it', () => {
  for (const refresh of ['10', '30', '60', 'off']) {
    const value = resolveResourceRoute({ refresh }, true)
    assert.equal(value.refresh, refresh)
    assert.deepEqual(value.query, { refresh })
    assert.equal(value.changed, false)
    assert.deepEqual(resolveResourceRoute({ refresh }).query, {})
  }
  for (const refresh of [undefined, '15', '1', '09', '10.0', '0', 'OFF', ['10'], 10]) {
    const value = resolveResourceRoute({ refresh }, true)
    assert.equal(value.refresh, '15')
    assert.deepEqual(value.query, {})
  }
})

test('CPU busy response must prove the fixed one-minute window and preserves valid zero', () => {
  const at = '2026-10-07T00:00:00Z', key = 'node.cpu.busy_percent'
  const response = { subject: { kind: 'node', node_id: 'node-id' }, end: at, queried_at: at, series: [{ metric_key: key, dimensions: {}, unit: 'percent', window_seconds: 60, points: [{ evaluated_at: at, sampled_at: at, value: 0, data_state: 'valid' }] }] }
  assert.equal(validateResourceResponse(response, 'node-id', [key]), response)
  assert.equal(currentResourceValue(response.series[0].points[0]), 0)
  assert.deepEqual(resolveResourceRoute({ metric: key }, true).query, { metric: key })
  for (const window of [undefined, 0, 15, '60']) {
    const value = structuredClone(response); value.series[0].window_seconds = window
    assert.throws(() => validateResourceResponse(value, 'node-id', [key]))
  }
})

test('filesystem URL selects a complete exact mount and scalar/list navigation clears it', () => {
  const dimensions = { device: '/dev/a " or up{', mountpoint: '/data space\t', fstype: 'ext4' }
  const query = { metric: 'node.filesystem.used_percent', ...dimensions, range: '7d', refresh: 'off' }
  assert.deepEqual(resolveResourceRoute(query, true).query, query)
  assert.deepEqual(resolveResourceRoute(query).dimensions, {})
  assert.deepEqual(resolveResourceRoute({ ...query, metric: 'node.load.average_1m' }, true).dimensions, {})
  const incomplete = resolveResourceRoute({ metric: query.metric, device: dimensions.device }, true)
  assert.equal(incomplete.metric, query.metric)
  assert.equal(incomplete.invalidDimensions, true)
  assert.deepEqual(incomplete.query, { metric: query.metric, device: dimensions.device })
  assert.deepEqual(trendParameters('node', query.metric, '7d', '2026-10-07T00:00:00Z', dimensions).device, dimensions.device)
})

test('filesystem series retain bind mounts and reject incomplete, foreign or fabricated dimensions', () => {
  const at = '2026-10-07T00:00:00Z', dimensions = { device: '/dev/a', mountpoint: '/', fstype: 'ext4' }
  const keys = filesystemMetrics.map(metric => metric.key)
  const series = filesystemMetrics.flatMap(metric => ['/', '/bind'].map(mountpoint => ({ metric_key: metric.key, dimensions: { ...dimensions, mountpoint }, unit: metric.unit, window_seconds: 0, points: [{ evaluated_at: at, sampled_at: at, value: 0, data_state: 'valid' }] })))
  const response = { subject: { kind: 'node', node_id: 'node-id' }, end: at, queried_at: at, series }
  assert.equal(validateResourceResponse(response, 'node-id', keys), response)
  assert.equal(filesystemRows(response).length, 2)
  for (const mutate of [value => value.series[0].dimensions = {}, value => value.series.push(value.series[0]), value => value.series[0].dimensions.private = 'secret', value => value.series.pop(), value => delete value.series[0].dimensions]) {
    const bad = structuredClone(response); mutate(bad)
    assert.throws(() => validateResourceResponse(bad, 'node-id', keys))
  }
  assert.throws(() => validateResourceResponse(response, 'node-id', keys, false, dimensions))
  const empty = { ...response, series: filesystemMetrics.map(metric => ({ metric_key: metric.key, dimensions: {}, unit: metric.unit, window_seconds: 0, points: [{ evaluated_at: at, sampled_at: null, value: null, data_state: 'no_data' }] })) }
  assert.equal(validateResourceResponse(empty, 'node-id', keys), empty)
  assert.deepEqual(filesystemRows(empty), [])
})
