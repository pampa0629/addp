import test from 'node:test'
import assert from 'node:assert/strict'
import { traceRoute, validateTraces } from '../src/utils/traces.js'
const owner = { module_name: 'monitor', instance_id: 'one' }
export function fixture() {
  const start = '2026-10-20T00:00:00Z', end = '2026-10-20T00:00:01Z'
  return { start, end, queried_at: end, policy_version: 1, coverage: 'sampled', data: [{ trace_id: '1'.repeat(32), start, end, duration_ms: 1000, partial: false, policy_version: 1, coverage: 'sampled', edges: [], spans: [{ span_id: '2'.repeat(16), parent_span_id: '', module_name: 'monitor', instance_id: 'one', role: 'backend', kind: 'server', method: 'GET', route: '/items/:id', status: 200, failed: false, start, end, duration_ms: 1000 }] }] }
}
test('Trace links preserve only bounded instance, ID and window inputs', () => {
  assert.equal(traceRoute({ instance_id: '1', trace_id: '1'.repeat(32), start: '2026-10-20T00:00:00Z', end: '2026-10-20T00:01:00Z' }).instanceID, 1)
  for (const q of [{ trace_id: '1'.repeat(32) }, { instance_id: '01' }, { instance_id: '1', query: '{}' }, { instance_id: '1', trace_id: '0'.repeat(32) }, { start: '2026-10-20T00:00:00Z', end: '2026-10-20T02:00:00Z' }]) assert.throws(() => traceRoute(q))
})
test('Trace response binds the selected historical owner and actual call edges', () => {
  const valid = fixture(); assert.equal(validateTraces(valid, owner), valid)
  for (const mutate of [v => { v.data[0].spans[0].instance_id = 'foreign' }, v => { v.data[0].edges = [{ client_span_id: '3'.repeat(16), server_span_id: '2'.repeat(16) }] }, v => { v.data[0].spans.push({ ...v.data[0].spans[0] }) }, v => { v.coverage = 'complete' }, v => { v.data[0].spans[0].route = '/private?secret=x' }]) {
    const v = fixture(); mutate(v); assert.throws(() => validateTraces(v, owner))
  }
})
