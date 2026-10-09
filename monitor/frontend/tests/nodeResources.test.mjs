import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { validateCollection, currentResourceValue, resourceChartRows, resolveResourceRoute, trendParameters, validateResourceResponse, filesystemMetrics, inodeMetrics, diskMetrics, networkMetrics, resourceDimensionRows, sortDiskResourceRows } from '../src/utils/nodeResources.js'

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
  assert.equal(resourceDimensionRows(response).length, 2)
  for (const mutate of [value => value.series[0].dimensions = {}, value => value.series.push(value.series[0]), value => value.series[0].dimensions.private = 'secret', value => value.series.pop(), value => delete value.series[0].dimensions]) {
    const bad = structuredClone(response); mutate(bad)
    assert.throws(() => validateResourceResponse(bad, 'node-id', keys))
  }
  assert.throws(() => validateResourceResponse(response, 'node-id', keys, false, dimensions))
  const empty = { ...response, series: filesystemMetrics.map(metric => ({ metric_key: metric.key, dimensions: {}, unit: metric.unit, window_seconds: 0, points: [{ evaluated_at: at, sampled_at: null, value: null, data_state: 'no_data' }] })) }
  assert.equal(validateResourceResponse(empty, 'node-id', keys), empty)
  assert.deepEqual(resourceDimensionRows(empty), [])
})


test('inode observations share mount identity and reject fractional or unsafe counts', () => {
  const at = '2026-10-07T00:00:00Z', dimensions = { device: '/dev/a', mountpoint: '/', fstype: 'ext4' }
  const keys = inodeMetrics.map(metric => metric.key)
  const series = inodeMetrics.map(metric => ({ metric_key: metric.key, dimensions, unit: metric.unit, window_seconds: 0, points: [{ evaluated_at: at, sampled_at: at, value: metric.name === 'inodeTotal' || metric.name === 'inodeFree' ? 100 : 0, data_state: 'valid' }] }))
  const response = { subject: { kind: 'node', node_id: 'node-id' }, series, end: at, queried_at: at }
  assert.equal(validateResourceResponse(response, 'node-id', keys), response)
  assert.equal(resourceDimensionRows(response, null).length, 1)
  for (const value of [0.5, Number.MAX_SAFE_INTEGER + 1]) {
    const bad = structuredClone(response); bad.series.find(row => row.unit === 'inodes').points[0].value = value
    assert.throws(() => validateResourceResponse(bad, 'node-id', keys))
  }
  const route = resolveResourceRoute({ metric: 'node.filesystem.inodes_used_percent', ...dimensions }, true)
  assert.deepEqual(route.dimensions, dimensions)
  assert.equal(route.invalidDimensions, false)
})

test('collection evidence separates source failure from unsupported coverage and rejects stale success', () => {
  const at = '2026-10-08T00:00:00Z'
  assert.equal(validateCollection({ state: 'failed', sampled_at: at, filesystem: 'unknown' }, at).state, 'failed')
  assert.equal(validateCollection({ state: 'collecting', sampled_at: at, filesystem: 'not_collected' }, at).filesystem, 'not_collected')
  assert.throws(() => validateCollection({ state: 'failed', sampled_at: at, filesystem: 'not_collected' }, at))
  assert.throws(() => validateCollection({ state: 'collecting', sampled_at: '2026-10-07T00:00:00Z', filesystem: 'available' }, at))
  assert.throws(() => validateCollection(undefined, at))
})


test('disk selectors use only device and reject foreign dimensions or incomplete device families', () => {
  const at = '2026-10-08T00:00:00Z', dimensions = { device: 'nvme0n1' }
  const query = { metric: diskMetrics[0].key, ...dimensions, range: '5m' }
  assert.deepEqual(resolveResourceRoute(query, true).query, query)
  assert.equal(resolveResourceRoute({ ...query, mountpoint: '/' }, true).invalidDimensions, true)
  assert.equal(resolveResourceRoute({ metric: diskMetrics[0].key }, true).invalidDimensions, true)
  const keys = diskMetrics.map(metric => metric.key)
  const response = { subject: { kind: 'node', node_id: 'node-id' }, end: at, queried_at: at, series: diskMetrics.map(metric => ({ metric_key: metric.key, dimensions, unit: metric.unit, window_seconds: 60, points: [{ evaluated_at: at, sampled_at: at, value: 0, data_state: 'valid' }] })) }
  assert.equal(validateResourceResponse(response, 'node-id', keys, false, dimensions), response)
  assert.equal(resourceDimensionRows(response).length, 1)
  for (const change of [bad => bad.series[0].dimensions = {}, bad => bad.series[0].dimensions = { device: 'other' }, bad => bad.series[0].dimensions.mountpoint = '/', bad => bad.series[0].window_seconds = 15, bad => bad.series[0].points[0].value = -1]) {
    const bad = structuredClone(response); change(bad)
    assert.throws(() => validateResourceResponse(bad, 'node-id', keys, false, dimensions))
  }
})

test('disk order prioritizes current IO evidence without hiding unknown devices or changing source rows', () => {
  const point = (value, data_state = 'valid') => ({ value, data_state })
  const row = (device, read, write, busy, duration = point(999)) => ({ key: device, dimensions: { device }, metrics: Object.fromEntries(diskMetrics.map((metric, index) => [metric.key, [read, write, busy, duration, duration][index]])) })
  const rows = [
    row('nbd10', point(0), point(0), point(0)),
    row('a-stale', point(999, 'stale'), point(0), point(0)),
    row('z-write', null, point(1), null),
    row('nbd2', point(0), point(0), point(0)),
    row('b-partial', point(0), null, point(0)),
    row('c-missing', point(null, 'no_data'), point(null, 'not_connected'), null),
    row('z-busy', null, null, point(0.01)),
    row('z-read', point(2), null, null)
  ]
  const snapshot = structuredClone(rows)
  const expected = ['z-busy', 'z-read', 'z-write', 'nbd2', 'nbd10', 'a-stale', 'b-partial', 'c-missing']
  assert.deepEqual(sortDiskResourceRows(rows).map(item => item.dimensions.device), expected)
  assert.deepEqual(sortDiskResourceRows([...rows].reverse()).map(item => item.dimensions.device), expected)
  assert.deepEqual(rows, snapshot)
  rows[2].metrics[diskMetrics[1].key] = point(9999)
  assert.deepEqual(sortDiskResourceRows(rows).map(item => item.dimensions.device), expected)
  assert.deepEqual(sortDiskResourceRows([]), [])
})


test('network observations require one interface and complete minute evidence, and never merge with disk devices', () => {
  const at = '2026-10-07T00:00:00Z', dims = { device: 'eth0' }
  const series = networkMetrics.map(metric => ({ metric_key: metric.key, dimensions: dims, unit: metric.unit, window_seconds: 60, points: [{ evaluated_at: at, sampled_at: at, value: 0, data_state: 'valid' }] }))
  const reply = { subject: { kind: 'node', node_id: 'node-id' }, end: at, queried_at: at, series }
  const keys = networkMetrics.map(metric => metric.key)
  assert.equal(validateResourceResponse(reply, 'node-id', keys), reply)
  const query = { metric: keys[0], device: 'eth0', range: '5m' }
  assert.deepEqual(resolveResourceRoute(query, true).query, query)
  for (const patch of [{ window_seconds: 0 }, { dimensions: { device: 'eth0', mountpoint: '/' } }, { unit: 'percent' }]) {
    const bad = structuredClone(reply); Object.assign(bad.series[0], patch)
    assert.throws(() => validateResourceResponse(bad, 'node-id', keys))
  }
  const disk = { ...reply, series: diskMetrics.map(metric => ({ ...series[0], metric_key: metric.key, unit: metric.unit })) }
  assert.equal(resourceDimensionRows(reply, disk).length, 2)
  const source = readFileSync(new URL('../src/views/NodeResources.vue', import.meta.url), 'utf8')
  assert.equal((source.match(/v-for="family in deviceSections"/g) || []).length, 1)
  assert.doesNotMatch(source, /function selectDisk|function selectNetwork/)
})


test('disk timing preserves valid idle busy and unknown duration with exact ms route', () => {
  const at = '2026-10-08T00:00:00Z', dims = { device: 'sda' }
  const series = diskMetrics.map(metric => ({ metric_key: metric.key, dimensions: dims, unit: metric.unit, window_seconds: 60, points: [{ evaluated_at: at, sampled_at: metric.unit === 'milliseconds' ? null : at, value: metric.unit === 'milliseconds' ? null : 0, data_state: metric.unit === 'milliseconds' ? 'no_data' : 'valid' }] }))
  const response = { subject: { kind: 'node', node_id: 'node-id' }, end: at, queried_at: at, series }
  assert.equal(validateResourceResponse(response, 'node-id', diskMetrics.map(metric => metric.key)), response)
  const metric = diskMetrics.find(metric => metric.name === 'diskReadDuration')
  assert.equal(resolveResourceRoute({ metric: metric.key, device: 'sda', range: '5m' }, true).metric, metric.key)
  assert.equal(currentResourceValue(series[3].points[0]), null)
  series[2].points[0].value = 101
  assert.throws(() => validateResourceResponse(response, 'node-id', diskMetrics.map(metric => metric.key)), /invalid_resource_response/)
})
