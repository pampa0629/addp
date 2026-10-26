import assert from 'node:assert/strict'
import test from 'node:test'
import { existsSync, readFileSync } from 'node:fs'
import { allResourceMetrics, hostLoadConnectionMetrics, hostListMetricSets, hostListMetrics, pressureMetrics, validateResourceSummaries, validateCollection, resourceMissingMessage, currentResourceValue, resourceChartRows, resolveResourceRoute, trendParameters, validateResourceResponse, filesystemMetrics, inodeMetrics, diskMetrics, networkMetrics, resourceDimensionRows, resourceDetailGroups, filesystemResourceState, sortDiskResourceRows, visibleDiskResourceRows } from '../src/utils/nodeResources.js'
import { supportsResourceComparison, resourceTrendComparison } from '../src/utils/nodeResources.js'
import { resourceTrendMetrics, resourceTrendChartRows } from '../src/utils/nodeResources.js'

test('device throughput requests both directions in one fixed scope without changing other metrics', () => {
  const end = '2026-10-07T00:00:00Z'
  for (const pair of [diskMetrics.slice(0, 2), networkMetrics]) for (const selected of pair) {
    const params = trendParameters('host', selected.key, '7d', end, { device: 'exact-device' })
    assert.equal(params.metrics, pair.map(metric => metric.key).join(','))
    assert.equal(params.device, 'exact-device')
    assert.equal(Date.parse(params.end) - Date.parse(params.start), 604800000)
  }
  for (const key of ['node.cpu.busy_percent', 'node.disk.io_busy_percent', 'node.filesystem.used_percent']) {
    assert.deepEqual(resourceTrendMetrics(key).map(metric => metric.key), [key])
  }
  assert.throws(() => resourceTrendMetrics('arbitrary.metric'))
})

test('paired throughput aligns by metric identity and retains independent zero, missing and stale points', () => {
  const at = '2026-10-07T00:00:00Z'
  for (const pair of [diskMetrics.slice(0, 2), networkMetrics]) {
    const lines = pair.map((metric, i) => ({ metric_key: metric.key, points: [
      { evaluated_at: at, value: i === 0 ? 0 : 2048, data_state: 'valid' },
      { evaluated_at: '2026-10-07T00:00:15Z', value: i === 0 ? 1e12 : 4096, data_state: i === 0 ? 'stale' : 'valid' },
      { evaluated_at: '2026-10-07T00:00:30Z', value: i === 0 ? 1024 : null, data_state: i === 0 ? 'valid' : 'no_data' }
    ] }))
    const rows = resourceTrendChartRows({ series: lines.toReversed() }, pair[1].key)
    assert.deepEqual(rows.map(row => [row[pair[0].name], row[pair[1].name]]), [[0, 2048], [null, 4096], [1024, null]])
    assert.deepEqual(rows.map(row => row.evaluated_at), lines[0].points.map(point => point.evaluated_at))
    for (const change of [
      value => value.series.pop(),
      value => value.series[1].points.pop(),
      value => value.series[1].points[1].evaluated_at = '2026-10-07T00:00:16Z'
    ]) {
      const invalid = structuredClone({ series: lines }); change(invalid)
      assert.throws(() => resourceTrendChartRows(invalid, pair[0].key))
    }
  }
})

test('node resource trends delegate chart rendering and formatting to the shared owners', () => {
  const source = readFileSync(new URL('../src/views/NodeResources.vue', import.meta.url), 'utf8')
  assert.match(source, /common-frontend\/chart\/src\/ChartRenderer.vue/)
  assert.match(source, /common-frontend\/basic\/src\/utils\/fieldPresentation.mjs/)
  assert.doesNotMatch(source, /echarts|setOption|Intl\.NumberFormat|localStorage/)
})

for (const metrics of [pressureMetrics, hostListMetrics, hostLoadConnectionMetrics]) test(`summary ${metrics.length}-key projection exactly matches the authorized page, saved versions and shared fresh anchor`, () => {
  const at = '2026-10-07T00:00:00Z', nodes = [{ node_id: 'a', version: 1 }, { node_id: 'b', version: 2 }]
  const reply = { data: nodes.map(node => ({ subject: { kind: 'node', node_id: node.node_id }, node_version: node.version, policy_version: 1, end: at, queried_at: at,
    collection: { state: 'collecting', sampled_at: at, filesystem: 'available', network: 'available' },
    series: metrics.map(metric => ({ metric_key: metric.key, dimensions: {}, unit: metric.unit, window_seconds: metric.windowSeconds || 0,
      points: [{ evaluated_at: at, sampled_at: at, value: 0, data_state: 'valid' }] })) })) }
  assert.equal(validateResourceSummaries(reply, nodes, metrics).get('a').series[0].points[0].value, 0)
  for (const mutate of [value => value.data.pop(), value => value.data.push(value.data[0]), value => value.data[1].subject.node_id = 'a',
    value => value.data[0].subject.node_id = 'foreign', value => value.data[0].node_version++, value => value.data[1].policy_version++,
    value => value.data[1].end = '2026-10-07T00:00:01Z', value => value.data[1].queried_at = '2026-10-07T00:00:01Z',
    value => value.data[0].series[0].points[0].sampled_at = '2026-10-06T23:58:00Z']) {
    const changed = structuredClone(reply); mutate(changed)
    assert.throws(() => validateResourceSummaries(changed, nodes, metrics))
  }
  assert.throws(() => validateResourceSummaries({ data: [] }, [], metrics))
  const other = metrics === pressureMetrics ? hostListMetrics : pressureMetrics
  assert.throws(() => validateResourceSummaries(reply, nodes, other))
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

function comparisonResponse(range = '5m', step = 15, values = (at, boundary) => at <= boundary ? 30 : 40) {
  const params = trendParameters('node-id', 'node.memory.used_percent', range, '2026-10-07T00:00:00Z', {}, true)
  const start = Date.parse(params.start), end = Date.parse(params.end), boundary = (start + end) / 2
  const points = Array.from({ length: Math.floor((end - start) / (step * 1000)) + 1 }, (_, index) => {
    const at = start + index * step * 1000, stamp = new Date(at).toISOString()
    return { evaluated_at: stamp, sampled_at: stamp, value: values(at, boundary), data_state: 'valid' }
  })
  return { ...params, subject: { kind: 'node', node_id: 'node-id' }, queried_at: params.end, step_seconds: step,
    series: params.metrics.split(',').sort(key => key.endsWith('used_percent') ? -1 : 1).map(key => ({ metric_key: key, dimensions: {}, unit: key.endsWith('used_percent') ? 'percent' : 'bytes', window_seconds: 0, points: points.map(point => ({ ...point, value: key.endsWith('used_percent') ? point.value : 2 ** 34 })) })) }
}

test('comparison restores only supported periods and metrics and uses one doubled window', () => {
  const input = { compare: 'previous', range: '24h', metric: 'node.cpu.busy_percent' }
  assert.deepEqual(resolveResourceRoute(input, true).query, input)
  assert.equal(resolveResourceRoute(input, true).compare, true)
  assert.equal(trendParameters('node', input.metric, '24h', '2026-10-07T00:00:00Z', {}, true).start, '2026-10-05T00:00:00.000Z')
  for (const patch of [{ range: '7d' }, { metric: 'node.memory.total_bytes' }, { compare: ['previous'] }, { compare: 'unknown' }]) {
    assert.equal(resolveResourceRoute({ ...input, ...patch }, true).compare, false)
    assert.equal(Object.hasOwn(resolveResourceRoute({ ...input, ...patch }, true).query, 'compare'), false)
  }
  assert.equal(resolveResourceRoute(input).compare, false)
  assert.equal(supportsResourceComparison(input.metric, '5m'), true)
  assert.equal(supportsResourceComparison(input.metric, '7d'), false)
  assert.throws(() => trendParameters('node', input.metric, '7d', '2026-10-07T00:00:00Z', {}, true))
})

test('comparison counts the boundary once and compares unrounded point means in percentage points', () => {
  const response = comparisonResponse()
  response.series[0].points[0].value = 100 // The left-open initial boundary belongs to neither period.
  const value = resourceTrendComparison(response, '5m')
  assert.equal(value.complete, true)
  assert.deepEqual([value.previous.valid, value.previous.total, value.current.valid, value.current.total], [20, 20, 20, 20])
  assert.equal(value.previous.mean, 30)
  assert.equal(value.current.mean, 40)
  assert.equal(value.delta, 10)
  assert.equal(value.current.start, '2026-10-06T23:55:00.000Z')
  const precision = resourceTrendComparison(comparisonResponse('5m', 15, (at, boundary) => at <= boundary ? 30.004 : 30.006), '5m')
  assert.ok(Math.abs(precision.delta - 0.002) < 1e-10)
  const zero = resourceTrendComparison(comparisonResponse('5m', 15, (at, boundary) => at <= boundary ? 0 : 40), '5m')
  assert.equal(zero.delta, 40)
})

test('any invalid evaluation point blocks both means and the comparison without inventing zero', () => {
  for (const state of ['stale', 'no_data', 'not_connected']) for (const index of [1, 21]) {
    const response = comparisonResponse(), point = response.series[0].points[index]
    Object.assign(point, { data_state: state, value: state === 'stale' ? point.value : null, sampled_at: state === 'stale' ? new Date(Date.parse(point.evaluated_at) - 61000).toISOString() : null })
    const result = resourceTrendComparison(response, '5m')
    assert.equal(result.complete, false)
    assert.equal(result.delta, null)
    assert.equal(result.previous.mean, null)
    assert.equal(result.current.mean, null)
    assert.equal(result.previous.valid + result.current.valid, 39)
  }
  const empty = resourceTrendComparison(comparisonResponse('5m', 600), '5m')
  assert.equal(empty.complete, false)
  assert.equal(empty.previous.total, 0)
})

test('comparison validates the entire evaluation grid and freshness before calculating', () => {
  for (const mutate of [
    row => row.step_seconds = 0, row => row.step_seconds = 15.5, row => row.step_seconds = 20,
    row => row.series[0].points.pop(), row => row.series[0].points.reverse(),
    row => row.series[0].points[1].evaluated_at = row.series[0].points[0].evaluated_at,
    row => row.series[0].points[5].sampled_at = row.start,
    row => row.series[0].points[1].sampled_at = row.end,
    row => row.start = row.end, row => row.series[0].metric_key = 'node.load.average_1m'
  ]) {
    const row = comparisonResponse(); mutate(row)
    assert.throws(() => resourceTrendComparison(row, '5m'))
  }
  const daily = resourceTrendComparison(comparisonResponse('24h', 180), '24h')
  assert.equal(daily.previous.total, 480)
  assert.equal(daily.current.total, 480)
  assert.equal(daily.complete, true)
  const uneven = resourceTrendComparison(comparisonResponse('5m', 45), '5m')
  assert.equal(uneven.previous.total, 6)
  assert.equal(uneven.current.total, 7)
  assert.equal(uneven.complete, true)
  assert.equal(uneven.delta, 10)
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


test('list and detail share one canonical bounded refresh choice', () => {
  for (const refresh of ['10', '30', '60', 'off']) {
    const value = resolveResourceRoute({ refresh }, true)
    assert.equal(value.refresh, refresh)
    assert.deepEqual(value.query, { refresh })
    assert.equal(value.changed, false)
    assert.deepEqual(resolveResourceRoute({ refresh }).query, { refresh })
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
  assert.equal(validateCollection({ state: 'failed', sampled_at: at, filesystem: 'unknown', network: 'unknown' }, at).state, 'failed')
  assert.equal(validateCollection({ state: 'collecting', sampled_at: at, filesystem: 'not_collected', network: 'not_collected' }, at).filesystem, 'not_collected')
  assert.throws(() => validateCollection({ state: 'failed', sampled_at: at, filesystem: 'not_collected', network: 'not_collected' }, at))
  assert.throws(() => validateCollection({ state: 'collecting', sampled_at: '2026-10-07T00:00:00Z', filesystem: 'available', network: 'available' }, at))
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

test('disk activity orders and collapses only proven idle devices while retaining unknown and selected rows', () => {
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
  const sorted = sortDiskResourceRows(rows)
  assert.deepEqual(visibleDiskResourceRows(sorted).map(item => item.dimensions.device), ['z-busy', 'z-read', 'z-write', 'a-stale', 'b-partial', 'c-missing'])
  assert.deepEqual(visibleDiskResourceRows(sorted, 'nbd2').map(item => item.dimensions.device), ['z-busy', 'z-read', 'z-write', 'nbd2', 'a-stale', 'b-partial', 'c-missing'])
  assert.deepEqual(visibleDiskResourceRows(sorted, 'not-present'), visibleDiskResourceRows(sorted))
  assert.deepEqual(visibleDiskResourceRows([rows[0], rows[3]]), [])
  assert.deepEqual(visibleDiskResourceRows([]), [])
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
  assert.equal((source.match(/<HostDeviceResources v-for="family in deviceFamilies"/g) || []).length, 1)
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


test('per-section explanations use collection evidence without claiming why a counter window is invalid', () => {
  for (const family of ['filesystem', 'network']) {
    for (const state of ['not_connected', 'no_sample', 'failed', 'stale']) assert.equal(resourceMissingMessage({ state, [family]: 'unknown' }, family), `monitor.resources.collection.${state}`)
    for (const [coverage, key] of [['not_collected', 'notCollected'], ['failed', 'collectorFailed'], ['available', 'empty'], ['unknown', 'unknown']]) assert.equal(resourceMissingMessage({ state: 'collecting', [family]: coverage }, family), `monitor.resources.${family}.${key}`)
  }
  const at = '2026-10-08T00:00:00Z'
  for (const network of [undefined, null, 'unsupported', 'available']) assert.throws(() => validateCollection({ state: 'failed', sampled_at: at, filesystem: 'unknown', network }, at))
  assert.equal(resourceMissingMessage(null, 'network'), '')
})

test('historical trend anchors are canonical, explicit and never auto-refresh', () => {
 const end = '2026-10-07T00:00:00.000Z'
 const value = resolveResourceRoute({ end, refresh: '15', metric: 'node.cpu.busy_percent' }, true)
 assert.equal(value.end, end); assert.equal(value.refresh, 'off'); assert.equal(value.invalidEnd, false)
 assert.deepEqual(value.query, { metric: 'node.cpu.busy_percent', end, refresh: 'off' })
 assert.equal(resolveResourceRoute(value.query, true).changed, false)
 for (const end of ['', 'bad', ['2026-10-07T00:00:00Z'], '2026-02-30T00:00:00Z', '2026-10-07T00:00:00.123Z']) {
   assert.equal(resolveResourceRoute({ end }, true).invalidEnd, true)
 }
 assert.deepEqual(resolveResourceRoute({ end }).query, {})
})

test('a historical curve cannot contain a different window, future sample or reordered points', async () => {
 const { validateResourceTrendWindow } = await import('../src/utils/nodeResources.js')
 const window = { start: '2026-10-06T23:00:00Z', end: '2026-10-07T00:00:00Z' }
 const response = { ...window, queried_at: '2026-10-07T00:15:00Z', series: [{ points: [
   { evaluated_at: '2026-10-06T23:59:45Z', sampled_at: '2026-10-06T23:59:45Z' },
   { evaluated_at: window.end, sampled_at: null }
 ] }] }
 assert.equal(validateResourceTrendWindow(response, window), response)
 for (const mutate of [r => r.end = r.queried_at, r => r.start = 'bad', r => r.queried_at = window.start,
  r => r.series[0].points.reverse(), r => r.series[0].points[0].sampled_at = window.end,
  r => r.series[0].points[0].evaluated_at = '2026-10-06T22:00:00Z']) {
   const bad = structuredClone(response); mutate(bad)
   assert.throws(() => validateResourceTrendWindow(bad, window))
 }
})


test('memory trends use a single aligned three-measure query while comparison reads only usage', () => {
  const keys = ['node.memory.total_bytes', 'node.memory.available_bytes', 'node.memory.used_percent']
  for (const key of keys) assert.equal(trendParameters('node-id', key, '5m', '2026-10-07T00:00:00Z').metrics, keys.join(','))
  const response = comparisonResponse()
  response.series.reverse()
  Object.assign(response.series.find(line => line.metric_key === keys[1]).points[1], { value: null, sampled_at: null, data_state: 'no_data' })
  const compared = resourceTrendComparison(response, '5m')
  assert.equal(compared.complete, true)
  assert.equal(compared.delta, 10)
  const rows = resourceTrendChartRows(response, keys[2])
  assert.deepEqual([rows[1].memoryTotal, rows[1].memoryAvailable, rows[1].memoryUsed], [2 ** 34, null, 30])
  const bad = structuredClone(response)
  bad.series[0].points[1].evaluated_at = bad.series[0].points[0].evaluated_at
  assert.throws(() => resourceTrendComparison(bad, '5m'))
  response.series.pop()
  assert.throws(() => resourceTrendChartRows(response, keys[2]))
})

test('TCP is a bounded integer host gauge, separate from device network rates and comparisons', () => {
  const metric = allResourceMetrics.find(item => item.key === 'node.tcp.current_connections')
  assert.equal(metric.unit, 'connections'); assert.equal(metric.precision, 0)
  const state = resolveResourceRoute({ metric: metric.key, device: 'eth0', range: '1h', compare: 'previous' }, true)
  assert.deepEqual(state.dimensions, {})
  assert.equal(state.compare, false)
  assert.deepEqual(resourceTrendMetrics(metric.key).map(item => item.key), [metric.key])
})


test('host list filters retain System enums across detail without widening the metric query', () => {
  const query = { page: '2', page_size: '10', search: 'Node_%', node_kind: 'virtual', enabled: 'false', refresh: 'off' }
  for (const detail of [false, true]) {
    const state = resolveResourceRoute(query, detail)
    assert.deepEqual(state.query, query)
    assert.equal(state.nodeKind, 'virtual')
    assert.equal(state.enabled, 'false')
    assert.equal(state.changed, false)
  }
  for (const value of [null, ['virtual'], 'all', 'VIRTUAL', true]) {
    const state = resolveResourceRoute({ node_kind: value, enabled: value })
    assert.equal(state.nodeKind, '')
    assert.equal(state.enabled, '')
    assert.deepEqual(state.query, {})
    assert.equal(state.changed, true)
  }
  const params = trendParameters('host', 'node.cpu.busy_percent', '5m', '2026-10-07T00:00:00Z')
  assert.equal('node_kind' in params, false)
  assert.equal('enabled' in params, false)
})


test('list views restore one canonical projection across detail without changing filters or pagination', () => {
  const query = { page: '2', page_size: '10', node_kind: 'virtual', enabled: 'false', search: 'VM', list_view: 'load-connections', refresh: 'off' }
  for (const detail of [false, true]) {
    const state = resolveResourceRoute(query, detail)
    assert.deepEqual(state.query, query)
    assert.equal(state.changed, false)
    assert.equal(state.listView, 'load-connections')
    assert.equal(hostListMetricSets[state.listView], hostLoadConnectionMetrics)
  }
  assert.deepEqual(hostLoadConnectionMetrics.map(metric => metric.key), ['node.load.average_1m', 'node.load.average_5m', 'node.load.average_15m', 'node.tcp.current_connections'])
  assert.equal(hostLoadConnectionMetrics.length * resolveResourceRoute().pageSize, 80)
  for (const list_view of ['resources', 'unknown', ['load-connections'], '', null]) {
    const state = resolveResourceRoute({ list_view })
    assert.deepEqual(state.query, {})
    assert.equal(hostListMetricSets[state.listView], hostListMetrics)
  }
})


test('host list and detail have one device presentation owner and use the fixed device catalog', () => {
  const page = readFileSync(new URL('../src/views/NodeResources.vue', import.meta.url), 'utf8')
  const inline = readFileSync(new URL('../src/components/HostResourceDetailObservation.vue', import.meta.url), 'utf8')
  const shared = readFileSync(new URL('../src/components/HostDeviceResources.vue', import.meta.url), 'utf8')
  for (const source of [page, inline]) {
    assert.match(source, /import HostDeviceResources from/)
    assert.doesNotMatch(source, /sortDiskResourceRows|resource-disk-toggle/)
  }
  assert.equal((shared.match(/prop="dimensions.device"/g) || []).length, 1)
  assert.match(shared, /visibleDiskResourceRows/)
  assert.match(shared, /formatValue\(row.metrics\[metric.key\], metric\)/)
  assert.match(inline, /createLatestRequestScope/)
  assert.match(inline, /createAutoRefresh/)
  assert.doesNotMatch(inline, /setInterval|localStorage|sessionStorage|Promise.all/)
})


test('filesystem list preserves separate fixed capacity and inode budgets and full mount identities', () => {
  assert.deepEqual(resourceDetailGroups.filesystem, [filesystemMetrics, inodeMetrics])
  assert.deepEqual(resourceDetailGroups.filesystem.map(group => group.length), [5, 4])
  const mount = { device: '/dev/a', mountpoint: '/data', fstype: 'ext4' }
  const mounts = [mount, { ...mount, fstype: 'xfs' }, { ...mount, device: '/dev/b' }]
  const reply = (metric, value) => ({ collection: { state: 'collecting', filesystem: 'available' }, series: mounts.map(dimensions => ({ metric_key: metric, dimensions, points: [{ data_state: 'valid', value }] })) })
  const capacity = reply(filesystemMetrics[1].key, 1024), inodes = reply(inodeMetrics[3].key, 0)
  inodes.series.reverse()
  const state = filesystemResourceState(capacity, inodes)
  assert.equal(state.rows.length, 3)
  assert.equal(new Set(state.rows.map(row => row.key)).size, 3)
  assert.equal(state.missingMessage, '')
  for (const row of state.rows) {
    assert.equal(row.metrics[filesystemMetrics[1].key].value, 1024)
    assert.equal(row.metrics[inodeMetrics[3].key].value, 0)
  }
  assert.equal(filesystemResourceState(null, inodes, 'budget').rows.length, 3)
  assert.equal(filesystemResourceState(capacity, null, '', 'unavailable').rows.length, 3)
  assert.equal(filesystemResourceState(null, null, 'budget').missingMessage, '')
  assert.equal(filesystemResourceState({ collection: { state: 'collecting', filesystem: 'not_collected' }, series: [] }, null).missingMessage, 'monitor.resources.filesystem.notCollected')
})

test('filesystem rows have one presentation owner shared by host list and detail', () => {
  const page = readFileSync(new URL('../src/views/NodeResources.vue', import.meta.url), 'utf8')
  const inline = readFileSync(new URL('../src/components/HostResourceDetailObservation.vue', import.meta.url), 'utf8')
  const shared = readFileSync(new URL('../src/components/HostFilesystemResources.vue', import.meta.url), 'utf8')
  for (const source of [page, inline]) {
    assert.match(source, /import HostFilesystemResources from/)
    assert.doesNotMatch(source, /data-testid="resource-filesystem-table"|prop="dimensions.mountpoint"/)
  }
  assert.equal((shared.match(/data-testid="resource-filesystem-table"/g) || []).length, 1)
  assert.match(shared, /filesystemResourceState/)
  assert.match(shared, /row-key="key"/)
  assert.equal(existsSync(new URL('../src/components/HostDeviceObservation.vue', import.meta.url)), false)
})
