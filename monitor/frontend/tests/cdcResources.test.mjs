import test from 'node:test'
import assert from 'node:assert/strict'
import { cdcMetrics, resolveCDCRoute, validateCDCResources } from '../src/utils/cdcResources.js'

const at = '2026-11-11T08:00:00Z'
function response() { return { discovery_state: 'available', start: at, end: at, queried_at: at, step_seconds: 15, policy_version: 1, lookback_seconds: 300, data: [{ subject: { kind: 'infrastructure_component', component_id: '10000000-0000-4000-8000-000000000001', component_type: 'kafka-connect', started_at: '2026-11-11T07:00:00Z' }, collection: { state: 'collecting', sampled_at: at }, series: cdcMetrics['kafka-connect'].map(metric => ({ metric_key: metric.key, unit: metric.unit, window_seconds: 0, dimensions: {}, points: [{ evaluated_at: at, sampled_at: at, value: metric.name === 'sourceDelay' ? -0.002 : 0, data_state: 'valid' }] })) }] } }
test('CDC route canonicalizes fixed stages, metrics and bounded refresh', () => {
  const route = resolveCDCRoute({ stage: 'redpanda', metric: 'up', range: 'forever', refresh: '1', page: '0', extra: 'drop' })
  assert.equal(route.changed, true); assert.equal(route.metric.key, 'cdc.redpanda.disk_used_percent'); assert.equal(route.refresh, '15'); assert.equal(route.range, '5m'); assert.deepEqual(route.query, { view: 'resources', stage: 'redpanda' })
  assert.equal(resolveCDCRoute(route.query).changed, false)
})
test('CDC validates identity, missing values and native negative delay without conflating empty discovery', () => {
  assert.equal(validateCDCResources(response()).data[0].series.at(2).points[0].value, -0.002)
  for (const mutate of [value => { value.data[0].subject.component_type = 'postgresql' }, value => { value.data.push(value.data[0]) }, value => { value.discovery_state = 'unavailable' }, value => { value.data[0].series[0].points[0].value = -1 }, value => { value.data[0].series[0].points[0].data_state = 'no_data' }, value => { value.data[0].series[0].metric_key = 'up' }]) {
    const value = response(); mutate(value); assert.throws(() => validateCDCResources(value), /invalid_resource_response/)
  }
  for (const state of ['available', 'unavailable']) assert.equal(validateCDCResources({ ...response(), discovery_state: state, data: [] }).discovery_state, state)
})
