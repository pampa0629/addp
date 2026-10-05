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

import { projectLineageFields, lineageFieldConnections } from '../../graph/src/lineageFields.js'
import { readFileSync } from 'node:fs'

const field = (item, name, hash = 'sha256:one') => ({ kind: 'field_ref', item_id: item, field_name: name, schema_snapshot_hash: hash })
test('table cards preserve schema versions, exact fields, generated rows and row endpoints', () => {
  const first = field(1, 'a.b'), second = field(1, 'generated'), historical = field(1, 'a.b', 'sha256:old'), target = field(2, 'out')
  const graph = projectLineageFields([first, second, historical, target], [{ source: first, target }, { source: historical, target }])
  assert.equal(graph.nodes.length, 3)
  assert.deepEqual(graph.nodes[0].fields, [first, second])
  assert.equal(graph.edges[0].sourceAnchor, 1)
  assert.equal(graph.edges[0].targetAnchor, 0)
  assert.notEqual(graph.edges[0].source, graph.edges[1].source)
  assert.equal(graph.nodes[0].anchors.length, 4)
})
test('collapsed cards keep precise fields, snapshots and every edge while routing to a summary row', () => {
  const first = field(1, 'nested.id'), second = field(1, 'nickname'), old = field(1, 'nickname', 'sha256:old'), target = field(2, 'out')
  const nodes = [first, second, old, target], edges = [{ source: first, target }, { source: second, target }, { source: old, target }]
  const expanded = projectLineageFields(nodes, edges)
  const collapsed = projectLineageFields(nodes, edges, new Set([expanded.nodes[0].id]))
  assert.equal(collapsed.nodes.length, expanded.nodes.length)
  assert.deepEqual(collapsed.edges, expanded.edges)
  assert.deepEqual(collapsed.nodes[0].fields, [first, second])
  assert.equal(collapsed.nodes[0].collapsed, true)
  assert.equal(collapsed.nodes[0].size[1], 106)
  assert.deepEqual(collapsed.nodes[0].anchors[0], collapsed.nodes[0].anchors[2])
  assert.deepEqual(collapsed.nodes[0].anchors[1], collapsed.nodes[0].anchors[3])
  assert.equal(collapsed.nodes[1].collapsed, false)
  assert.deepEqual(collapsed.nodes[1], expanded.nodes[1])
  assert.deepEqual(projectLineageFields(nodes, edges, new Set()), expanded)
})
test('field focus follows a chain without bringing in its siblings and stops cycles', () => {
  const a = field(1, 'a'), b = field(2, 'b'), c = field(3, 'c'), sibling = field(4, 'sibling')
  const edges = [{ source: a, target: b }, { source: b, target: c }, { source: a, target: sibling }, { source: c, target: b }]
  const focus = lineageFieldConnections(edges, lineageNodeId(b))
  assert.deepEqual([...focus.connections].sort(), ['lineage-edge:0', 'lineage-edge:1', 'lineage-edge:3'])
  assert.equal(focus.fields.has(lineageNodeId(sibling)), false)
})
test('the shared viewer owns field grouping and Manager sends a single table graph query', () => {
  const viewer = readFileSync(new URL('../../graph/src/LineageViewer.vue', import.meta.url), 'utf8')
  const host = readFileSync(new URL('../../../manager/frontend/src/components/explorer/ItemPanel.vue', import.meta.url), 'utf8')
  const office = readFileSync(new URL('../src/components/previews/OfficePreview.vue', import.meta.url), 'utf8')
  assert.match(viewer, /useElementFullscreen/)
  assert.match(office, /useElementFullscreen/)
  assert.doesNotMatch(viewer + office + host, /requestFullscreen\(/)
  assert.match(viewer, /useDAGViewport/)
  assert.match(viewer, /createDAGDragNodeBehavior/)
  assert.doesNotMatch(host, /updateLayout|drag-node|collapsedTables|toggleFields/)
  assert.match(viewer, /projectLineageFields/)
  assert.match(viewer, /lineageFieldConnections/)
  assert.doesNotMatch(viewer, /update:field|v-for="name in fields"/)
  assert.match(host, /granularity: lineageGranularity.value/)
  assert.doesNotMatch(host, /field_name:|subject_kind:.*field_ref/)
})
