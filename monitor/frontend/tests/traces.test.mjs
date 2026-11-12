import test from 'node:test'
import assert from 'node:assert/strict'
import { traceRoute, validateTraces, validateTraceSamples, traceLinkQuery, traceTimeline } from '../src/utils/traces.js'
const owner = { module_name: 'monitor', instance_id: 'one' }
test('sample states bind the complete current page and the exact inspection window', () => {
  const window = { start: '2026-11-12T00:00:00Z', end: '2026-11-12T00:15:00Z' }
  const value = { ...window, queried_at: window.end, policy_version: 2, coverage: 'sampled', data: [
    { instance_id: 1, state: 'has_samples' }, { instance_id: 2, state: 'not_found' }, { instance_id: 3, state: 'unavailable' }
  ] }
  assert.deepEqual(validateTraceSamples(value, [1, 2, 3], window), { 1: 'has_samples', 2: 'not_found', 3: 'unavailable' })
  for (const mutate of [
    v => { v.data[0].state = 'zero' }, v => { v.data[0].instance_id = 4 },
    v => { v.data[0].instance_id = 2 }, v => { v.data.pop() },
    v => { v.start = '2026-11-12T00:01:00Z' }, v => { v.coverage = 'complete' },
    v => { v.queried_at = v.start }
  ]) {
    const bad = structuredClone(value)
    mutate(bad)
    assert.throws(() => validateTraceSamples(bad, [1, 2, 3], window))
  }
})
export function fixture() {
  const start = '2026-10-20T00:00:00Z', end = '2026-10-20T00:00:01Z'
  return { start, end, queried_at: end, policy_version: 2, coverage: 'sampled', data: [{ trace_id: '1'.repeat(32), start, end, duration_ms: 1000, partial: false, policy_version: 2, coverage: 'sampled', edges: [], spans: [{ span_id: '2'.repeat(16), parent_span_id: '', module_name: 'monitor', instance_id: 'one', role: 'backend', kind: 'server', scope: 'addp.http', attempt: 0, links: [], method: 'GET', route: '/items/:id', status: 200, failed: false, start, end, duration_ms: 1000 }] }] }
}
test('Trace links preserve only bounded instance, ID and window inputs', () => {
  assert.equal(traceRoute({ instance_id: '1', trace_id: '1'.repeat(32), start: '2026-10-20T00:00:00Z', end: '2026-10-20T00:01:00Z' }).instanceID, 1)
  assert.equal(traceRoute({ trace_id: '1'.repeat(32) }).instanceID, 0)
  assert.equal(traceRoute({ range: '30m' }).query.range, '30m')
  for (const query of [{ range: '24h' }, { range: '1h', start: '2026-10-20T00:00:00Z', end: '2026-10-20T00:15:00Z' }]) assert.throws(() => traceRoute(query))
  for (const q of [{ trace_id: 'invalid' }, { instance_id: '01' }, { instance_id: '1', query: '{}' }, { instance_id: '1', trace_id: '0'.repeat(32) }, { start: '2026-10-20T00:00:00Z', end: '2026-10-20T02:00:00Z' }]) assert.throws(() => traceRoute(q))
})
test('Trace response binds the selected historical owner and actual call edges', () => {
  const valid = fixture(); assert.equal(validateTraces(valid, owner), valid)
  assert.equal(validateTraces(valid, undefined, '1'.repeat(32)), valid)
  for (const mutate of [v => { v.data[0].spans[0].instance_id = 'foreign' }, v => { v.data[0].edges = [{ client_span_id: '3'.repeat(16), server_span_id: '2'.repeat(16) }] }, v => { v.data[0].spans.push({ ...v.data[0].spans[0] }) }, v => { v.coverage = 'complete' }, v => { v.data[0].spans[0].route = '/private?secret=x' }]) {
    const v = fixture(); mutate(v); assert.throws(() => validateTraces(v, owner))
  }
})

test('Trace summary selects the longest matched cross-module server span', () => {
  const value = fixture()
  const trace = value.data[0]
  const [entry] = trace.spans
  const client = { ...entry, span_id: '3'.repeat(16), parent_span_id: entry.span_id, kind: 'client', route: '', duration_ms: 900 }
  const system = { ...entry, span_id: '4'.repeat(16), parent_span_id: client.span_id, module_name: 'system', instance_id: 'two', duration_ms: 500 }
  const develop = { ...system, span_id: '5'.repeat(16), module_name: 'develop', instance_id: 'three', duration_ms: 600 }
  const sameModule = { ...entry, span_id: '6'.repeat(16), parent_span_id: client.span_id, duration_ms: 800 }
  const unmatched = { ...system, span_id: '7'.repeat(16), parent_span_id: '8'.repeat(16), duration_ms: 750 }
  trace.spans.push(client, system, develop, sameModule, unmatched)
  trace.edges = [system, develop, sameModule].map(server => ({
    client_span_id: client.span_id, server_span_id: server.span_id,
    from_module: client.module_name, from_instance: client.instance_id,
    to_module: server.module_name, to_instance: server.instance_id
  }))
  validateTraces(value, owner)
  assert.equal(traceTimeline(trace).longestDownstream, develop)
  trace.edges = trace.edges.slice(2)
  assert.equal(traceTimeline(trace).longestDownstream, undefined)
})

test('HTTP failure summary picks the deepest observed downstream server of the selected span', () => {
  const value = fixture(), trace = value.data[0], [entry] = trace.spans
  const makeSpan = (digit, parent, module, kind) => ({
    ...entry, span_id: digit.repeat(16), parent_span_id: parent.span_id,
    module_name: module, instance_id: module, kind, status: 503, failed: true
  })
  const entryCall = makeSpan('3', entry, 'monitor', 'client')
  const selected = makeSpan('4', entryCall, 'system', 'server')
  const selectedCall = makeSpan('5', selected, 'system', 'client')
  const downstream = makeSpan('6', selectedCall, 'develop', 'server')
  const downstreamCall = makeSpan('7', downstream, 'develop', 'client')
  const deepest = makeSpan('8', downstreamCall, 'meta', 'server')
  const sibling = makeSpan('9', entryCall, 'service', 'server')
  const disconnected = makeSpan('a', selectedCall, 'agent', 'server')
  trace.spans.push(entryCall, selected, selectedCall, downstream, downstreamCall, deepest, sibling, disconnected)
  trace.edges = [[entryCall, selected], [selectedCall, downstream], [downstreamCall, deepest], [entryCall, sibling]].map(([client, server]) => ({
    client_span_id: client.span_id, server_span_id: server.span_id,
    from_module: client.module_name, from_instance: client.instance_id,
    to_module: server.module_name, to_instance: server.instance_id
  }))
  validateTraces(value, owner)
  assert.equal(traceTimeline(trace, selected).downstreamFailure, deepest)
  trace.edges.splice(2, 1)
  assert.equal(traceTimeline(trace, selected).downstreamFailure, downstream)
  trace.edges.splice(1, 1)
  assert.equal(traceTimeline(trace, selected).downstreamFailure, undefined)
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
