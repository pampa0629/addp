import test from 'node:test'
import assert from 'node:assert/strict'
import { engineMetrics, engineSummaryMetrics, resolveEngineRoute, validateEnginePage, validateEngineResources, engineTrendParameters } from '../src/utils/engineResources.js'

const id = '10000000-0000-4000-8000-000000000001', node = '10000000-0000-4000-8000-000000000002'
const owner = { deployment_id: id, display_name: 'Infra PostgreSQL', version: 1, enabled: true, node_id: node, host_binding_state: 'bound', engine_type: 'postgresql' }
const at = '2026-10-20T14:00:00Z'
function response(metrics = engineSummaryMetrics) {
  return { queried_at: at, start: at, end: at, step_seconds: 15, policy_version: 1, lookback_seconds: 300, data: [{ subject: { kind: 'engine_deployment', deployment_id: id, display_name: owner.display_name, engine_type: 'postgresql' }, node_id: node, collection: { state: 'collecting', sampled_at: at }, series: metrics.map(metric => ({ metric_key: metric.key, unit: metric.unit, window_seconds: metric.windowSeconds, dimensions: {}, points: [{ evaluated_at: at, sampled_at: at, value: 0, data_state: 'valid' }] })) }] }
}

test('PostgreSQL catalog has twelve metrics and three explicit one-minute rates', () => {
  assert.equal(engineMetrics.length, 12)
  assert.equal(new Set(engineMetrics.map(item => item.key)).size, 12)
  assert.equal(engineMetrics.filter(item => item.windowSeconds === 60).length, 3)
  assert.deepEqual(engineSummaryMetrics.map(item => item.name), ['total', 'active', 'committed', 'capacity'])
})
test('engine URL retains exact deployment, metric, range and bounded refresh', () => {
  const input = { deployment_id: id, metric: engineMetrics[11].key, range: '7d', refresh: 'off', search: 'Infra', page: '2' }
  assert.equal(resolveEngineRoute(input).changed, false)
  assert.deepEqual(resolveEngineRoute(input).query, input)
  assert.deepEqual(resolveEngineRoute({ deployment_id: 'foreign', metric: 'up', range: '30d', refresh: '1', page: '-1', extra: 'x' }).query, {})
})
test('current page rejects duplicate, malformed and wrong-page owner references', () => {
  const page = { data: [owner], page: 1, page_size: 20, total: 1 }
  assert.equal(validateEnginePage(page, 1), page)
  for (const value of [{ ...page, data: [owner, owner], total: 2 }, { ...page, page: 2 }, { ...page, data: [{ ...owner, node_id: '' }] }, { ...page, data: [{ ...owner, engine_type: 'redis' }] }]) assert.throws(() => validateEnginePage(value, 1), /invalid_resource_response/)
})
test('valid zero is preserved; foreign, stale, fractional connection or incomplete evidence is rejected', () => {
  const good = response()
  assert.equal(validateEngineResources(good, [owner], engineSummaryMetrics), good)
  for (const mutate of [
    value => { value.data[0].subject.deployment_id = node },
    value => { value.data[0].collection.state = 'snapshot_unavailable' },
    value => { value.data[0].series[0].points[0].value = 0.5 },
    value => { value.data[0].series[0].points[0].sampled_at = '2026-10-20T13:58:00Z' },
    value => { value.data[0].series[0].unit = 'percent' },
    value => { value.data[0].series.pop() },
    value => { value.data[0].series[0].metric_key = 'node.cpu.busy_percent' }
  ]) { const value = response(); mutate(value); assert.throws(() => validateEngineResources(value, [owner], engineSummaryMetrics), /invalid_resource_response/) }
})
test('current-container trend accepts clipping and null gaps without inventing zero', () => {
  const metric = engineMetrics[8], params = engineTrendParameters(id, metric.key, '7d', at), value = response([metric])
  value.start = '2026-10-20T13:59:30Z'
  value.data[0].collection = { state: 'no_sample', sampled_at: null }
  value.data[0].series[0].points = ['2026-10-20T13:59:30Z', '2026-10-20T13:59:45Z', at].map((at, index) => ({ evaluated_at: at, sampled_at: index === 1 ? null : at, value: index === 1 ? null : 0, data_state: index === 1 ? 'no_data' : 'valid' }))
  assert.equal(validateEngineResources(value, [owner], [metric], params), value)
  value.data[0].series[0].points[1].value = 0
  assert.throws(() => validateEngineResources(value, [owner], [metric], params), /invalid_resource_response/)
})
