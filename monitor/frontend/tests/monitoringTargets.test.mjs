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
