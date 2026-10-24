import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { platformAlertState, platformAlertQuery } from '../src/utils/platformAlerts.js'
test('platform alert state retains only bounded canonical management filters', () => {
 const query = { tab: 'notifications', category: 'resource', status: 'resolved', subject_id: 'host-id', page: '2', delivery_page: '3' }
 assert.deepEqual(platformAlertQuery(platformAlertState(query)), query)
 for (const query of [{ tab: ['notifications'], page: '-1', delivery_page: '01', category: 'tenant', status: 'secret', secret: 'private' }, { page: '10001', subject_id: '\nunsafe' }]) {
  assert.deepEqual(platformAlertQuery(platformAlertState(query)), {})
 }
 assert.deepEqual(platformAlertQuery(platformAlertState({ status: 'all' })), { status: 'all' })
})
test('System has no second incident or notification management owner', () => {
 const read = path => readFileSync(new URL(path, import.meta.url), 'utf8')
 const diagnostics = read('../../../system/frontend/src/components/ModuleLogPipeline.vue')
 const client = read('../../../system/frontend/src/api/logPipeline.js')
 assert.match(diagnostics, /openConsoleRoute\('\/monitor\/platform-alerts/)
 for (const source of [diagnostics, client]) assert.doesNotMatch(source, /log-notification-|alert-notification-|acknowledge\(|retryDelivery\(|destinationDraft|pipeline-incidents/)
 assert.doesNotMatch(read('../src/api/platformAlerts.js'), /log-notification-|log-pipeline\/incidents/)
})
test('resource rules retain a canonical independent page and reuse the only host selector', () => {
 assert.deepEqual(platformAlertQuery(platformAlertState({ tab: 'rules', rule_page: '3' })), { tab: 'rules', rule_page: '3' })
 assert.deepEqual(platformAlertQuery(platformAlertState({ tab: 'rules', rule_page: '01' })), { tab: 'rules' })
 const read = path => readFileSync(new URL(path, import.meta.url), 'utf8')
 for (const path of ['../src/views/MonitoringTargets.vue','../src/components/ResourceAlertRules.vue']) {
  assert.match(read(path), /<HostNodeSelect/u)
  assert.doesNotMatch(read(path), /remote-method="searchNodes"/u)
 }
 assert.match(read('../src/views/MonitoringTargets.vue'), /platform\.host_node\.read/u)
 assert.match(read('../src/components/ResourceAlertRules.vue'), /platform\.host_node\.read/u)
})

test('resource evidence stays frozen and only valid host signals build historical navigation', async () => {
 const { resourceAlertEvidence, resourceAlertHostID, resourceAlertTrendRoute } = await import('../src/utils/platformAlerts.js')
 const row = { category: 'resource', subject_kind: 'host_node', subject_id: 'f83e9874-2164-4411-a5c0-650d143006cf', signal: 'node.memory.used_percent', resource_evidence: {
  rule_id: 'a0e9964c-e2d3-46ba-b151-52249cff528e', rule_version: 2, node_version: 1, target_id: '1621bb62-cb0e-40e1-b988-0d4d36683e51', target_version: 1, query_policy_version: 1,
  policy: { trigger_percent: 80, recovery_percent: 75, trigger_seconds: 300, recovery_seconds: 120 },
  evaluated_at: '2026-10-07T00:00:00.789Z', sampled_from: '2026-10-06T23:55:00Z', sampled_to: '2026-10-07T00:00:00Z', samples: 21, minimum: 81, maximum: 96, latest: 90
 } }
 assert.equal(resourceAlertEvidence(row), row.resource_evidence)
 assert.equal(resourceAlertHostID(row), row.subject_id)
 const defaults = structuredClone(row); defaults.resource_evidence.query_policy_version = 0
 assert.equal(resourceAlertEvidence(defaults), defaults.resource_evidence)
 assert.deepEqual(resourceAlertTrendRoute(row), { path: `/node-resources/${row.subject_id}`, query: { metric: row.signal, end: '2026-10-07T00:00:00.000Z', refresh: 'off' } })
 for (const mutate of [r => r.category = 'log_pipeline', r => r.subject_kind = 'process', r => r.subject_id = '../unsafe', r => r.signal = 'arbitrary', r => r.resource_evidence = null,
  r => r.resource_evidence.latest = NaN, r => r.resource_evidence.minimum = 97, r => r.resource_evidence.samples = 0,
  r => r.resource_evidence.policy.trigger_percent = 74, r => r.resource_evidence.sampled_to = '2026-10-07T00:00:01Z', r => r.resource_evidence.policy.trigger_seconds = 14]) {
   const bad = structuredClone(row); mutate(bad)
   assert.equal(resourceAlertEvidence(bad), null)
   assert.equal(resourceAlertTrendRoute(bad), null)
 }
})

test('host references deduplicate, bound concurrent reads and stop after invalidation', async () => {
 const { loadHostReferences } = await import('../src/utils/hostReferences.js')
 const ids = Array.from({ length: 9 }, (_, i) => `f83e9874-2164-4411-a5c0-${String(i).padStart(12, '0')}`)
 let active = 0, maximum = 0, current = true
 const seen = []
 const read = async id => {
   seen.push(id); maximum = Math.max(maximum, ++active)
   await new Promise(resolve => setImmediate(resolve)); active--
   if (id === ids[1]) throw new Error('denied')
   return { node_id: id === ids[2] ? ids[0] : id, display_name: 'Host' }
 }
 const result = await loadHostReferences([...ids, ids[0], 'invalid'], read, () => current)
 assert.equal(maximum, 4); assert.deepEqual(seen, ids)
 assert.deepEqual(Object.keys(result), ids.filter(id => id !== ids[1] && id !== ids[2]))
 seen.length = 0
 const stale = await loadHostReferences(ids, async id => { seen.push(id); current = false; return { node_id: id, display_name: 'late' } }, () => current)
 assert.deepEqual(stale, {}); assert.equal(seen.length, 4)
})


test('monitor management views share the sole bounded host-reference resolver', () => {
 for (const path of ['../src/views/PlatformAlerts.vue', '../src/views/MonitoringTargets.vue', '../src/components/ResourceAlertRules.vue', '../src/views/ProcessResources.vue', '../src/views/EngineResources.vue']) {
   const source = readFileSync(new URL(path, import.meta.url), 'utf8')
   assert.match(source, /loadHostReferences/u)
   assert.doesNotMatch(source, /Promise\.allSettled/u)
 }
})


test('incident severity survives routing and invalid conditions never broaden to all levels', () => {
 for (const severity of ['critical', 'warning', 'info']) {
  const query = { severity, category: 'process_resource', status: 'resolved', page: '2' }
  assert.deepEqual(platformAlertQuery(platformAlertState(query)), query)
 }
 assert.deepEqual(platformAlertQuery(platformAlertState({ severity: '' })), {})
 for (const severity of [['critical', 'warning'], 'x'.repeat(33), null]) {
  assert.equal(platformAlertState({ severity }).severity, 'invalid')
  assert.equal(platformAlertQuery(platformAlertState({ severity })).severity, 'invalid')
 }
})
