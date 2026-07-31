import assert from 'node:assert/strict'
import { test } from 'vitest'
import { placeIncrementalNodes } from '../src/utils/incrementalGraphLayout.js'

test('增量布局保持已有节点固定，并按父节点和 hop 放置新节点', () => {
  const data = {
    nodes: [
      { id: 'root' },
      { id: 'peer', x: 420, y: 0 },
      { id: 'child-a' },
      { id: 'child-b' },
      { id: 'grandchild' },
    ],
    edges: [
      { source: 'root', target: 'child-a' },
      { source: 'root', target: 'child-b' },
      { source: 'child-a', target: 'grandchild' },
    ],
  }
  const existing = new Map([
    ['root', { x: 0, y: 0 }],
    ['peer', { x: 420, y: 0 }],
  ])

  assert.equal(placeIncrementalNodes(data, existing, 'root'), true)
  assert.deepEqual({ x: data.nodes[0].x, y: data.nodes[0].y }, { x: 0, y: 0 })
  assert.deepEqual({ x: data.nodes[1].x, y: data.nodes[1].y }, { x: 420, y: 0 })

  const byID = new Map(data.nodes.map(node => [node.id, node]))
  const distance = node => Math.hypot(node.x, node.y)
  assert.ok(distance(byID.get('grandchild')) > distance(byID.get('child-a')))
  assert.notDeepEqual(
    { x: byID.get('child-a').x, y: byID.get('child-a').y },
    { x: byID.get('child-b').x, y: byID.get('child-b').y },
  )
})
