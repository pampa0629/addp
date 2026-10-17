import test from 'node:test'
import assert from 'node:assert/strict'
import { parseHostAdmission } from '../src/utils/hostAdmission.js'

const manifest = () => ({ schema: 'addp.host-admission/v1', host: {
  display_name: 'Docker Desktop Linux VM', node_kind: 'virtual', addresses: [], allowed_module_bindings: []
}, collection: { monitor_kind: 'host_resources', source: { type: 'node_exporter', endpoint: 'https://127.0.0.1:19091/metrics' } } })

test('a deployment manifest contains facts but no allocated host identity or credentials', () => {
  assert.deepEqual(parseHostAdmission(JSON.stringify(manifest())), manifest())
  const declared = manifest()
  declared.host.addresses = ['node.example', '10.0.0.8']
  declared.host.allowed_module_bindings = [{ module_name: 'manager', client_id: 'addp-manager' }]
  assert.deepEqual(parseHostAdmission(JSON.stringify(declared)), declared)
})

test('manifest version, field ownership, bounded shape and field types are closed', () => {
  for (const change of [
    value => { value.schema = 'addp.host-admission/v2' },
    value => { value.node_id = '10000000-0000-4000-8000-000000000001' },
    value => { value.host.node_id = '10000000-0000-4000-8000-000000000001' },
    value => { value.host.node_kind = 'container' },
    value => { value.host.addresses = ['same', 'same'] },
    value => { value.host.addresses = Array.from({ length: 33 }, (_, i) => `node-${i}.example`) },
    value => { value.host.allowed_module_bindings = [{ module_name: 'manager', client_id: 'addp-system' }] },
    value => { value.host.allowed_module_bindings = [{ module_name: 'manager', client_id: 'addp-manager', secret: 'forbidden' }] },
    value => { value.host.display_name = ' '.repeat(3) },
    value => { value.host.display_name = 'name\ncontrol' },
    value => { value.host.display_name = 'x'.repeat(256) },
    value => { value.host.addresses = ['http://host:80/path'] },
    value => { value.collection.enabled = true },
    value => { value.collection.monitor_kind = 'container_resources' },
    value => { value.collection.source.type = 'cadvisor' },
    value => { value.collection.source.tls_dir = '/private/source' }
  ]) {
    const value = manifest(); change(value)
    assert.throws(() => parseHostAdmission(JSON.stringify(value)), /invalid_host_admission/)
  }
  for (const value of ['', '{', 'null', '[]', '{}', JSON.stringify('x'.repeat(16384))]) assert.throws(() => parseHostAdmission(value), /invalid_host_admission/)
})

test('source is only an explicit HTTPS metrics address; no credential, query or port guessing', () => {
  for (const endpoint of ['http://127.0.0.1:9100/metrics', 'https://host/metrics', 'https://host:0/metrics',
    'https://host:65536/metrics', 'https://user:secret@host:9100/metrics', 'https://host:9100/metrics?q=x',
    'https://host:9100/metrics#part', 'https://host:9100/other', 'https://host:9100/metrics/']) {
    const value = manifest(); value.collection.source.endpoint = endpoint
    assert.throws(() => parseHostAdmission(JSON.stringify(value)), /invalid_host_admission/)
  }
  const value = manifest(); value.collection.source.endpoint = 'https://[::1]:9100/metrics'
  assert.equal(parseHostAdmission(JSON.stringify(value)).collection.source.endpoint, value.collection.source.endpoint)
})
