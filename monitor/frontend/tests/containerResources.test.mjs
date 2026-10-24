import test from 'node:test'
import assert from 'node:assert/strict'
import { containerMetrics, validateContainerObservation } from '../src/utils/containerResources.js'
const owner = { id: 7, module_name: 'monitor', instance_id: 'actual', role: 'worker', node_id: '' }
const at = '2026-10-24T00:00:00Z'
const response = () => ({ proof_state: 'verified', proof_observed_at: at, cpu_upper_cores: .5, memory_upper_bytes: 128 * 2 ** 20, cpu_window_verified: true, observation: { collection: { state: 'collecting', sampled_at: at }, subject: { kind: 'module_instance', ...owner }, node_id: '', queried_at: at, lookback_seconds: 300, policy_version: 1, series: containerMetrics.map(metric => ({ metric_key: metric.key, unit: metric.unit, window_seconds: metric.window, dimensions: {}, points: [{ evaluated_at: at, sampled_at: at, value: 0, data_state: 'valid' }] })) } })
test('container view preserves valid zero and real values above 100 without confusing process RSS', () => {
  const data = response(); data.observation.series[2].points[0].value = 120
  assert.equal(validateContainerObservation(data, owner), data)
  for (const alter of [d => { d.observation.subject.id = 9 }, d => { d.cpu_window_verified = false }, d => { d.cpu_upper_cores = null }, d => { d.memory_upper_bytes = 2 ** 53 }, d => { d.proof_observed_at = '2026-10-24T00:01:00Z' }, d => { d.observation.series[0].metric_key = 'process.cpu.core_equivalents' }, d => { d.observation.series[1].points[0].value = NaN }, d => { d.proof_state = 'unverified' }]) {
    const copy = response(); alter(copy); assert.throws(() => validateContainerObservation(copy, owner))
  }
})
test('unverified groups keep only absolute usage, with no guessed quota or ratio', () => {
  const data = response(); Object.assign(data, { proof_state: 'unverified', proof_observed_at: null, cpu_upper_cores: null, memory_upper_bytes: null, cpu_window_verified: false })
  for (const series of data.observation.series.slice(2)) Object.assign(series.points[0], { value: null, sampled_at: null, data_state: 'no_data' })
  assert.equal(validateContainerObservation(data, owner), data)
})
