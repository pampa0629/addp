import test from 'node:test'
import assert from 'node:assert/strict'
import { resolveProcessRoute, validateProcessInstances, validateProcessSummaries, processMetrics, processRoleLabelKey, processTrendParameters, validateProcessTrend } from '../src/utils/processResources.js'

const owner = { id: 7, module_name: 'monitor', instance_id: 'native-a', role: 'worker', status: 'up', node_id: '', process_metrics_declared: true }
const stamp = '2026-10-10T00:00:00Z'
const response = () => ({ data: [{ subject: { kind: 'module_instance', id: 7, module_name: 'monitor', instance_id: 'native-a', role: 'worker' }, node_id: '', policy_version: 1, lookback_seconds: 300, queried_at: stamp, collection: { state: 'collecting', sampled_at: stamp }, series: processMetrics.map(metric => ({ metric_key: metric.key, unit: metric.unit, window_seconds: metric.windowSeconds, dimensions: {}, points: [{ evaluated_at: stamp, sampled_at: stamp, value: metric.name === 'cpu' ? 1.5 : 4096, data_state: 'valid' }] })) }] })
test('worker purpose comes only from known runtime metadata and never replaces a different formal role', () => {
  for (const runtime of ['scan', 'sync', 'continuous_sync']) {
    assert.equal(processRoleLabelKey({ ...owner, metadata: { runtime_name: runtime } }), `monitor.process.workerTypes.${runtime}`)
    for (const role of ['backend', 'scheduler', 'ingress']) assert.equal(processRoleLabelKey({ ...owner, role, metadata: { runtime_name: runtime } }), `monitor.process.roles.${role}`)
  }
  for (const metadata of [undefined, null, {}, 'scan', { runtime_name: null }, { runtime_name: ['scan'] }, { runtime_name: 1 }, { runtime_name: 'future_worker' }, { runtime_name: ' scan' }, { runtime_name: '__proto__' }]) {
    assert.equal(processRoleLabelKey({ ...owner, module_name: 'meta', instance_id: 'continuous_sync', runtime_hostname: 'scan', metadata }), 'monitor.process.roles.worker')
  }
})
test('process trend scope stays separate from System list filters and rejects foreign, off-grid or invented samples', () => {
  const route = resolveProcessRoute({ instance_id: '7', range: '1h', role: 'worker' })
  assert.deepEqual(route.query, { role: 'worker', instance_id: '7', range: '1h' })
  assert.equal(route.params.instance_id, undefined)
  assert.deepEqual(resolveProcessRoute({ instance_id: '01', range: '7d' }).query, {})
  assert.deepEqual(resolveProcessRoute({ instance_id: '7', range: ['1h'] }).query, { instance_id: '7' })
  assert.throws(() => processTrendParameters(7, ['5m'], stamp), /invalid_resource_response/)
  const params = processTrendParameters(7, '5m', stamp)
  const start = '2026-10-09T23:59:45Z'
  const trendOwner = { ...owner, process_started_at: start }
  const history = () => ({ subject: response().data[0].subject, node_id: '', policy_version: 1, lookback_seconds: 300, queried_at: stamp, start, end: stamp, step_seconds: 15, series: processMetrics.slice(0, 2).map(metric => ({ metric_key: metric.key, unit: metric.unit, window_seconds: metric.windowSeconds, dimensions: {}, points: [{ evaluated_at: start, sampled_at: start, value: 0, data_state: 'valid' }, { evaluated_at: stamp, sampled_at: null, value: null, data_state: 'no_data' }] })) })
  assert.equal(validateProcessTrend(history(), trendOwner, params).series[0].points[0].value, 0)
  for (const change of [v => { v.subject.instance_id = 'previous' }, v => { v.series[0].points[1].value = 0 }, v => { v.start = '2026-10-08T23:59:45Z' }, v => { v.series[0].points[1].evaluated_at = start }, v => { v.series[1].metric_key = v.series[0].metric_key }, v => { v.series[0].unit = 'percent' }]) {
    const invalid = history(); change(invalid); assert.throws(() => validateProcessTrend(invalid, trendOwner, params), /invalid_resource_response/)
  }
  const submillisecond = { ...owner, process_started_at: '2026-10-09T23:59:44.000001Z' }
  assert.equal(validateProcessTrend(history(), submillisecond, params).start, start)
})
test('service route has bounded, canonical System filters', () => {
  assert.deepEqual(resolveProcessRoute({ page: '2', module_name: 'monitor', role: 'worker' }).query, { page: '2', module_name: 'monitor', role: 'worker' })
  assert.deepEqual(resolveProcessRoute({ page: '01', role: 'other', status: 'invalid', source: 'private' }).query, {})
  assert.deepEqual(resolveProcessRoute({ status: 'up', process_metrics_declared: 'true' }).query, {})
  assert.deepEqual(resolveProcessRoute().params, { page: 1, page_size: 20, status: 'up', process_metrics_declared: 'true' })
  assert.deepEqual(resolveProcessRoute({ status: 'all', process_metrics_declared: 'all' }).params, { page: 1, page_size: 20 })
  assert.deepEqual(resolveProcessRoute({ status: 'down', process_metrics_declared: 'false', page: '2' }).params, { page: 2, page_size: 20, status: 'down', process_metrics_declared: 'false' })
  assert.equal(validateProcessInstances({ data: [owner], total: 1, page: 1, page_size: 20 }, 1).data[0], owner)
  for (const filters of [{ status: 'down' }, { process_metrics_declared: 'false' }]) assert.throws(() => validateProcessInstances({ data: [owner], total: 1, page: 1, page_size: 20 }, 1, filters), /invalid_resource_response/)
})
test('process resource evidence binds exact owner and accepts multi-core and true zero', () => {
  const input = response()
  assert.equal(validateProcessSummaries(input, [owner]).get(7).series[0].points[0].value, 1.5)
  input.data[0].series[0].points[0].value = 0
  assert.equal(validateProcessSummaries(input, [owner]).get(7).series[0].points[0].value, 0)
  for (const change of [row => { row.subject.instance_id = 'foreign' }, row => { row.node_id = 'inferred-host' }, row => { row.series[0].unit = 'percent' }, row => { row.series[0].points[0].value = NaN }, row => { row.collection.state = 'identity_mismatch' }, row => { row.queried_at = '2026-10-10T00:01:01Z' }]) {
    const invalid = response(); change(invalid.data[0]); assert.throws(() => validateProcessSummaries(invalid, [owner]), /invalid_resource_response/)
  }
})
