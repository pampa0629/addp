import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { currentResourceValue, resourceChartRows, resolveResourceRoute, trendParameters, validateResourceResponse } from '../src/utils/nodeResources.js'

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
  const response = { subject: { kind: 'node', node_id: 'node-id' }, end: time, queried_at: time, series: [{ metric_key: key, unit: 'percent', points: [{ evaluated_at: time, sampled_at: time, value: 0, data_state: 'valid' }] }] }
  assert.equal(validateResourceResponse(response, 'node-id', [key]), response)
  assert.throws(() => validateResourceResponse(response, 'other-node', [key]))
  assert.throws(() => validateResourceResponse(response, 'node-id', [key, 'node.cpu.logical_cores']))
  for (const change of [{ value: null }, { sampled_at: null }, { data_state: 'unknown' }, { value: Infinity }]) {
    const value = structuredClone(response); Object.assign(value.series[0].points[0], change)
    assert.throws(() => validateResourceResponse(value, 'node-id', [key]))
  }
})
