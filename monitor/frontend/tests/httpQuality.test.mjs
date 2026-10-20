import test from 'node:test'
import assert from 'node:assert/strict'
import { resolveHTTPRoute, validateHTTPResponse, httpMetrics, httpValue, sortHTTPInterfaces } from '../src/utils/httpQuality.js'

const stamp = '2026-10-20T00:00:00Z', owner = { id: 1, module_name: 'gateway', instance_id: 'ingress-one', role: 'ingress' }
const response = () => ({ start: stamp, end: stamp, step_seconds: 15, window_seconds: 60, data: [{ subject: { ...owner, kind: 'module_instance' }, policy_version: 1, queried_at: stamp, collection: { state: 'collecting', sampled_at: stamp }, observations: [{ route: '', method: '', series: httpMetrics.map(metric => ({ metric_key: metric.key, unit: metric.unit, window_seconds: metric.name === 'active' ? 0 : 60, dimensions: {}, points: [{ evaluated_at: stamp, sampled_at: metric.name === 'qps' || metric.name === 'active' ? stamp : null, value: metric.name === 'qps' || metric.name === 'active' ? 0 : null, data_state: metric.name === 'qps' || metric.name === 'active' ? 'valid' : 'no_data' }] })) }] }] })
test('HTTP routes separate ingress and backend, retain exact templates and drop foreign parameters', () => {
  assert.deepEqual(resolveHTTPRoute({ role: 'ingress', instance_id: '1', route: '/items/:id', method: 'GET', query: 'private', page: '01' }).query, { role: 'ingress', instance_id: '1', route: '/items/:id', method: 'GET' })
  assert.equal(resolveHTTPRoute({ role: 'worker', window: '61' }).role, 'backend')
  assert.equal(resolveHTTPRoute({ instance_id: '1', route: '/private', method: 'UNBOUNDED' }).route, null)
  assert.equal(resolveHTTPRoute({ instance_id: '1', route: '', method: '_OTHER' }).route, '')
})
test('zero traffic preserves unavailable ratios and rejects stale, foreign or partial quality data', () => {
  const implicit = response(); implicit.data[0].policy_version = 0; validateHTTPResponse(implicit, [owner], 60)
  const value = validateHTTPResponse(response(), [owner], 60)
  assert.equal(httpValue(value.data[0].observations[0], 'qps'), 0)
  assert.equal(httpValue(value.data[0].observations[0], 'failure'), null)
  for (const change of [v => { v.data[0].subject.instance_id = 'previous' }, v => { v.data[0].collection.state = 'failed' }, v => { v.data[0].observations[0].series[1].points[0].value = 0 }, v => { v.data[0].observations[0].series[0].points[0].sampled_at = '2026-10-19T00:00:00Z' }]) {
    const invalid = response(); change(invalid); assert.throws(() => validateHTTPResponse(invalid, [owner], 60))
  }
  const a = { ...value.data[0].observations[0], route: '/known' }, b = structuredClone(a)
  b.route = '/unknown'; b.series[0].points[0] = { ...b.series[0].points[0], value: null, sampled_at: null, data_state: 'no_data' }
  assert.deepEqual(sortHTTPInterfaces([b, a], 'qps').map(row => row.route), ['/known', '/unknown'])
})

test('HTTP evidence binds the requested template, collection freshness and server ranking limit', () => {
  const value = response(), row = value.data[0].observations[0]
  row.route = '/items/:id'; row.method = 'GET'; row.series.pop()
  assert.equal(validateHTTPResponse(value, [owner], 60, false, { route: row.route, method: 'GET' }), value)
  assert.throws(() => validateHTTPResponse(value, [owner], 60, false, { route: '/foreign', method: 'GET' }))
  value.ranking_limit = 10; validateHTTPResponse(value, [owner], 60, true)
  value.ranking_limit = 100; assert.throws(() => validateHTTPResponse(value, [owner], 60, true))
  const stale = response(); stale.data[0].collection.sampled_at = '2026-10-19T23:58:00Z'
  assert.throws(() => validateHTTPResponse(stale, [owner], 60))
  const future = response(); future.data[0].queried_at = '2026-10-19T23:59:59Z'
  assert.throws(() => validateHTTPResponse(future, [owner], 60))
})
