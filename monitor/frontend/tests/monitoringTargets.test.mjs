import assert from 'node:assert/strict'
import test from 'node:test'
import { resolveTargetRoute, targetInput } from '../src/utils/monitoringTargets.js'
const node = 'f83e9874-2164-4411-a5c0-650d143006cf'
test('only bounded canonical target pagination survives URL restoration', () => {
  assert.deepEqual(resolveTargetRoute({ page: '2', page_size: '50' }), { page: 2, pageSize: 50, query: { page: '2', page_size: '50' }, changed: false })
  for (const query of [{ page: ['2','3'] }, { page: '01', page_size: '101' }, { page: '-1', endpoint: 'secret' }, { page: '1000001' }]) {
    assert.equal(resolveTargetRoute(query).changed, true)
    assert.deepEqual(resolveTargetRoute(query).query, {})
  }
})
test('edits preserve the saved subject, category and CAS version', () => {
  const form = { node_id: node, monitor_kind: 'container_resources', endpoint: ' https://example.test:9443/metrics ', enabled: false }
  const target = { subject: { node_id: node }, monitor_kind: 'host_resources', version: 8 }
  assert.deepEqual(targetInput(form, target), { version: 8, subject: { kind: 'node', node_id: node }, monitor_kind: 'host_resources', source: { type: 'node_exporter', endpoint: 'https://example.test:9443/metrics' }, enabled: false })
  assert.equal(targetInput(form).source.type, 'cadvisor')
  assert.equal(Object.hasOwn(targetInput(form), 'version'), false)
  for (const bad of ['invalid', node.toUpperCase(), '00000000-0000-0000-0000-000000000000']) assert.equal(targetInput({ ...form, node_id: bad }), null)
})

test('saved target evidence requires the same identity and version, including list summaries', async () => {
  const { validateTargetCollectionReference } = await import('../src/utils/monitoringTargets.js')
  const target = { id: '1621bb62-cb0e-40e1-b988-0d4d36683e51', version: 2 }
  const evidence = { target_id: target.id, target_saved_version: 2, collection: { state: 'collecting' } }
  assert.equal(validateTargetCollectionReference(evidence, target), evidence)
  assert.throws(() => validateTargetCollectionReference({ ...evidence, target_saved_version: 1 }, target), /target_collection_changed/)
  assert.throws(() => validateTargetCollectionReference({ ...evidence, target_id: node }, target), /target_collection_changed/)
  assert.throws(() => validateTargetCollectionReference({ collection: { state: 'collecting' } }, target), /invalid_resource_response/)
  assert.throws(() => validateTargetCollectionReference(evidence, { ...target, version: 0 }), /invalid_resource_response/)
  const disconnected = { collection: { state: 'not_connected' } }
  assert.equal(validateTargetCollectionReference(disconnected, target), disconnected)
})
