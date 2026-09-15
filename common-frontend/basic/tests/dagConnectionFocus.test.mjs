import assert from 'node:assert/strict'
import test from 'node:test'
import { focusDAGConnections } from '../../dag/src/utils/connections.js'

function item(type, model) {
  const render = { opacity: 1 }
  return { getType: () => type, getModel: () => model, getID: () => model.id,
    getContainer: () => ({ attr: (key, value) => { render[key] = value } }),
    toFront() {}, render }
}

test('DAG focus isolates one parallel connection and restores every item without changing models', () => {
  const nodes = ['a', 'b', 'c'].map(id => item('node', { id }))
  const edges = [item('edge', { id: 'ab-1', source: 'a', target: 'b' }), item('edge', { id: 'ab-2', source: 'a', target: 'b' }), item('edge', { id: 'bc', source: 'b', target: 'c' })]
  const graph = { getNodes: () => nodes, getEdges: () => edges, paint() {} }
  const before = JSON.stringify([...nodes, ...edges].map(node => node.getModel()))
  assert.deepEqual(focusDAGConnections(graph, edges[0]), [edges[0]])
  assert.equal(edges[0].render.opacity, 1)
  assert.equal(edges[1].render.opacity, 0.12)
  assert.equal(nodes[2].render.opacity, 0.25)
  assert.equal(focusDAGConnections(graph, nodes[1]).length, 3)
  focusDAGConnections(graph, null)
  assert.ok([...nodes, ...edges].every(node => node.render.opacity === 1))
  assert.equal(JSON.stringify([...nodes, ...edges].map(node => node.getModel())), before)
})
