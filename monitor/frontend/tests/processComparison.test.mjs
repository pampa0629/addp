import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { processMetrics, processTrendParameters, processTrendComparisons, resolveProcessRoute } from '../src/utils/processResources.js'
const end = '2026-10-10T00:00:00Z'
const owner = { id: 1, module_name: 'monitor', instance_id: 'current-worker', role: 'worker', node_id: '', process_started_at: '2026-10-07T00:00:00Z' }
function history(range = '5m', started = owner.process_started_at) {
  const params = processTrendParameters(1, range, end, true)
  const start = Math.max(Date.parse(params.start), Math.ceil(Date.parse(started) / 1000) * 1000)
  const boundary = (Date.parse(params.start) + Date.parse(end)) / 2
  const step = Math.max(15, Math.ceil((Date.parse(end) - Date.parse(params.start)) / 999 / 15000) * 15)
  return { params, owner: { ...owner, process_started_at: started }, value: { subject: { kind: 'module_instance', id: 1, module_name: 'monitor', instance_id: 'current-worker', role: 'worker' }, node_id: '', policy_version: 1, lookback_seconds: 300, queried_at: end, start: new Date(start).toISOString(), end, step_seconds: step, series: processMetrics.slice(0, 2).map(metric => ({ metric_key: metric.key, unit: metric.unit, window_seconds: metric.windowSeconds, dimensions: {}, points: Array.from({ length: Math.floor((Date.parse(end) - start) / (step * 1000)) + 1 }, (_, i) => { const at = start + i * step * 1000; return { evaluated_at: new Date(at).toISOString(), sampled_at: new Date(at).toISOString(), data_state: 'valid', value: metric.unit === 'cores' ? at <= boundary ? 0.125 : 1.25 : at <= boundary ? 2 * 1024 ** 3 : 1024 ** 3 } }) })) } }
}
const compare = row => processTrendComparisons(row.value, row.owner, row.params, row.range || '5m')
for (const range of ['5m', '1h', '24h']) test(`${range} compares one doubled window in original units, independent of series order`, () => {
  const row = history(range); row.range = range; row.value.series.reverse()
  const result = compare(row)
  assert.equal(result[processMetrics[0].key].delta, 1.125)
  assert.equal(result[processMetrics[1].key].delta, -(1024 ** 3))
  for (const value of Object.values(result)) { assert.equal(value.complete, true); assert.equal(value.previous.valid, value.previous.total); assert.equal(value.current.valid, value.current.total) }
})
test('comparison is canonical, opt-in, bounded and excluded from System list filters', () => {
  const state = resolveProcessRoute({ instance_id: '1', range: '1h', compare: 'previous', node_ip: '192.0.2.7' })
  assert.equal(state.compare, true); assert.equal(state.changed, false); assert.equal(state.params.compare, undefined)
  assert.equal(state.params.node_ip, '192.0.2.7')
  for (const input of [{ compare: 'previous' }, { instance_id: '1', range: '7d', compare: 'previous' }, { instance_id: '1', compare: ['previous'] }, { instance_id: '1', compare: 'other' }]) {
    const route = resolveProcessRoute(input); assert.equal(route.compare, false); assert.equal(route.query.compare, undefined)
  }
  assert.throws(() => processTrendParameters(1, '7d', end, true))
  assert.equal(processTrendParameters(1, '5m', end).start, '2026-10-09T23:55:00Z')
  assert.equal(processTrendParameters(1, '5m', end, true).start, '2026-10-09T23:50:00Z')
})
test('startup clipping and CPU warm-up never manufacture complete history', () => {
  for (const start of ['2026-10-09T23:50:00.000001Z', '2026-10-09T23:53:01Z', end]) {
    const row = history('5m', start)
    // Fractional timestamps are rounded upwards by the real service.
    if (start.includes('000001')) { row.value.start = '2026-10-09T23:50:01Z'; for (const line of row.value.series) { line.points = line.points.slice(0, -1).map(p => ({ ...p, evaluated_at: new Date(Date.parse(p.evaluated_at) + 1000).toISOString(), sampled_at: new Date(Date.parse(p.sampled_at) + 1000).toISOString() })) } }
    for (const value of Object.values(compare(row))) { assert.equal(value.complete, false); assert.equal(value.delta, null); assert.equal(value.previous.total, 20); assert.equal(value.current.total, 20) }
  }
  const row = history('5m', '2026-10-09T23:50:00Z'), result = compare(row)
  assert.equal(result[processMetrics[0].key].complete, false)
  assert.equal(result[processMetrics[1].key].complete, true)
})
test('either-period missing, stale or inactive points block only their own metric', () => {
  for (const index of [1, 21]) for (const state of ['no_data', 'stale', 'not_active', 'not_connected']) {
    const row = history(), p = row.value.series[0].points[index]
    Object.assign(p, { data_state: state, value: state === 'stale' ? 99 : null, sampled_at: state === 'stale' ? new Date(Date.parse(p.evaluated_at) - 61000).toISOString() : null })
    const result = compare(row)
    assert.equal(result[processMetrics[0].key].delta, null); assert.equal(result[processMetrics[0].key].previous.mean, null)
    assert.equal(result[processMetrics[1].key].complete, true)
  }
})
test('foreign identity, wrong grid, units and stale valid values are rejected before comparison', () => {
  for (const mutate of [x => x.subject.instance_id = 'previous', x => x.node_id = 'foreign', x => x.series[0].unit = 'percent', x => x.series[0].points.pop(), x => x.series[0].points[1].evaluated_at = x.start, x => x.series[0].points[10].sampled_at = x.start]) {
    const row = history(); mutate(row.value); assert.throws(() => compare(row))
  }
})
test('host and service comparison calculation and cards each have a single owner', () => {
  for (const name of ['nodeResources', 'processResources']) {
    const source = readFileSync(new URL(`../src/utils/${name}.js`, import.meta.url), 'utf8')
    assert.match(source, /evaluationPeriodComparison\.mjs/)
    assert.doesNotMatch(source, /previousSum|currentSum/)
  }
  for (const name of ['NodeResources', 'ProcessResources']) {
    const source = readFileSync(new URL(`../src/views/${name}.vue`, import.meta.url), 'utf8')
    assert.match(source, /ResourcePeriodComparison/)
    assert.doesNotMatch(source, /v-for="period in/)
  }
})

// CPU rate eligibility is evaluated at the curve timestamp. The latest scrape
// can legitimately precede it; its freshness is validated independently.
test('an eligible CPU evaluation is not demoted by scrape lag near the startup boundary', () => {
  const row = history('5m', '2026-10-09T23:49:14.900Z')
  const first = row.value.series[0].points[1]
  first.sampled_at = new Date(Date.parse(first.evaluated_at) - 500).toISOString()
  const result = compare(row)
  assert.equal(result[processMetrics[0].key].complete, true)
  assert.equal(result[processMetrics[0].key].delta, 1.125)
})
