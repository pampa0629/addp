import assert from 'node:assert/strict'
import test from 'node:test'
import { lineageNodeId, normalizeLineageGraph } from '../../graph/src/lineageApi.js'

test('field identity keeps table, exact field name and schema snapshot separate', () => {
  const node = { kind: 'field_ref', item_id: 3, field_name: 'a.b', schema_snapshot_hash: 'sha256:one' }
  assert.notEqual(lineageNodeId(node), lineageNodeId({ ...node, field_name: 'a' }))
  assert.notEqual(lineageNodeId(node), lineageNodeId({ ...node, schema_snapshot_hash: 'sha256:two' }))
  assert.notEqual(lineageNodeId(node), lineageNodeId({ ...node, item_id: 4 }))
  assert.notEqual(lineageNodeId(node), lineageNodeId({ kind: 'data_item', item_id: 3 }))
  assert.equal(lineageNodeId({ ...node, schema_snapshot_hash: '' }), '')
})

test('unavailable field evidence remains distinct from an observed field without edges', () => {
  assert.equal(normalizeLineageGraph({ field_lineage_status: 'unavailable' }).field_lineage_status, 'unavailable')
  assert.equal(normalizeLineageGraph({ field_lineage_status: 'complete', edges: [] }).field_lineage_status, 'complete')
  assert.equal(normalizeLineageGraph().field_lineage_status, '')
})
