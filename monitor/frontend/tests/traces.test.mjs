import test from 'node:test'
import assert from 'node:assert/strict'
import { traceRoute, validateTraces, traceLinkQuery } from '../src/utils/traces.js'
const owner = { module_name: 'monitor', instance_id: 'one' }
export function fixture() {
  const start = '2026-10-20T00:00:00Z', end = '2026-10-20T00:00:01Z'
  return { start, end, queried_at: end, policy_version: 2, coverage: 'sampled', data: [{ trace_id: '1'.repeat(32), start, end, duration_ms: 1000, partial: false, policy_version: 2, coverage: 'sampled', edges: [], spans: [{ span_id: '2'.repeat(16), parent_span_id: '', module_name: 'monitor', instance_id: 'one', role: 'backend', kind: 'server', scope: 'addp.http', attempt: 0, links: [], method: 'GET', route: '/items/:id', status: 200, failed: false, start, end, duration_ms: 1000 }] }] }
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

function executionFixture() {
  const value = fixture(), span = value.data[0].spans[0]
  Object.assign(span, { scope: 'addp.execution', kind: 'internal', phase: 'terminal', attempt: 3, outcome: 'failed', failed: true, method: '', route: '', status: 0, links: [{ trace_id: 'a'.repeat(32), span_id: 'b'.repeat(16), module_name: 'system', instance_id: 'two', role: 'backend', started_at: '2026-10-19T22:00:00Z', runtime_instance_id: 2 }] })
  return value
}
test('Execution stages accept only their root, finite outcomes and bounded authorized links', () => {
  const value = executionFixture(); assert.equal(validateTraces(value, owner), value)
  for (const mutate of [
    v => { v.policy_version = 1 },
    v => { v.data[0].spans[0].kind = 'server' },
    v => { v.data[0].spans[0].parent_span_id = 'a'.repeat(16) },
    v => { v.data[0].spans[0].attempt = 0 },
    v => { v.data[0].spans[0].outcome = 'private error' },
    v => { v.data[0].spans[0].links[0].runtime_instance_id = 0 },
    v => { v.data[0].spans[0].links[0].trace_id = v.data[0].trace_id },
    v => { v.data[0].spans[0].links[0].sql = 'private' },
    v => { v.data[0].spans[0].links[0].module_name = null },
    v => { v.data[0].spans[0].links[0].instance_id = null },
    v => { v.data[0].spans[0].links[0].span_id = 1234567890123456 },
    v => { v.data[0].spans[0].links.push({ ...v.data[0].spans[0].links[0] }) },
    v => { v.data[0].spans[0].links = Array(3).fill(v.data[0].spans[0].links[0]) },
    v => { v.data[0].spans[0].scope = 'addp.http' }
  ]) { const bad = executionFixture(); mutate(bad); assert.throws(() => validateTraces(bad, owner)) }
  for (const outcome of ['success','cancelled','rejected','timeout']) {
    const valid = executionFixture(); Object.assign(valid.data[0].spans[0], { outcome, failed: outcome === 'timeout' }); assert.equal(validateTraces(valid, owner), valid)
  }
})
test('Following a causal link uses the target start and a new authorized query', () => {
  const link = executionFixture().data[0].spans[0].links[0], now = Date.parse('2026-10-20T00:00:00Z')
  const query = traceLinkQuery(link, now)
  assert.equal(query.instance_id, '2'); assert.equal(query.trace_id, link.trace_id)
  assert.equal(query.start, '2026-10-19T21:59:59.000Z'); assert.equal(query.end, '2026-10-19T22:00:01.000Z')
  assert.equal(traceRoute(query).instanceID, 2)
  assert.equal(Date.parse(traceLinkQuery({ ...link, started_at: new Date(now).toISOString() }, now).end), now)
  for (const started_at of [new Date(now+1).toISOString(), new Date(now-72*3600000-1).toISOString()]) assert.throws(() => traceLinkQuery({ ...link, started_at }, now))
})
