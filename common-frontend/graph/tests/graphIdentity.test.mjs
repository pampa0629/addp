import assert from 'node:assert/strict'
import { test } from 'node:test'
import { graphNodeItemId, graphEdgeItemId } from '../src/graphIdentity.js'

test('overlapping opaque native IDs occupy separate G6 node and relationship namespaces', () => {
  const ids = ['0', '1', 'edge:0', 'node:0', '中文/图:0']
  const items = [...ids.map(graphNodeItemId), ...ids.map(graphEdgeItemId)]
  assert.equal(new Set(items).size, ids.length * 2)
  const nodes = new Map(ids.map(id => [graphNodeItemId(id), { nativeId: id }]))
  assert.equal(nodes.get(graphNodeItemId('0')).nativeId, '0')
  assert.equal(nodes.has(graphEdgeItemId('0')), false)
})
