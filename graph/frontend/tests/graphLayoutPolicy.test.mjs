import assert from 'node:assert/strict'
import { test } from 'vitest'
import { DagreLayout } from '@antv/layout'
import { createGraphLayoutConfig, isLargeGraph } from '../src/utils/graphLayoutPolicy.js'

test('大图布局使用轻量参数', () => {
  assert.equal(isLargeGraph(120, 80), true)
  assert.equal(isLargeGraph(60, 180), true)
  assert.equal(isLargeGraph(60, 80), false)

  const force = createGraphLayoutConfig('force', 200, 190)
  assert.equal(force.type, 'force')
  assert.equal(force.preventOverlap, false)
  assert.ok(force.alphaDecay >= 0.08)

  const circular = createGraphLayoutConfig('circular', 200, 190)
  assert.equal(circular.ordering, null)

  const radial = createGraphLayoutConfig('radial', 200, 190, 'selected-node')
  assert.equal(radial.focusNode, 'selected-node')
  assert.equal(radial.preventOverlap, false)
  assert.ok(radial.maxIteration <= 80)
  assert.equal(radial.maxPreventOverlapIteration, 0)
})

test('小图布局保留可读性优先的配置', () => {
  const force = createGraphLayoutConfig('force', 30, 35)
  assert.equal(force.preventOverlap, true)

  const circular = createGraphLayoutConfig('circular', 30, 35)
  assert.equal(circular.ordering, 'degree')

  const radial = createGraphLayoutConfig('radial', 30, 35)
  assert.equal(radial.preventOverlap, true)
  assert.ok(radial.maxIteration > 80)
})


test('dense cyclic graph layout fits visible nodes and relations without shrinking below semantic visibility', () => {
  const nodes = Array.from({ length: 12 }, (_, index) => ({ id: 'node-'+index, size: 44 }))
  const edges = nodes.flatMap((node, index) => nodes.slice(index+1).map(target => ({ source: node.id, target: target.id })))
  edges.push({ source: nodes[1].id, target: nodes[0].id }, { source: nodes[0].id, target: nodes[1].id }, { source: nodes[11].id, target: nodes[11].id })
  const layout = new DagreLayout(createGraphLayoutConfig('dagre', nodes.length, edges.length))
  layout.layout({ nodes, edges })
  const width = Math.max(...nodes.map(node => node.x)) - Math.min(...nodes.map(node => node.x)) + 44
  const height = Math.max(...nodes.map(node => node.y)) - Math.min(...nodes.map(node => node.y)) + 44
  const fit = Math.min(1, (754-60)/width, (775-60)/height)
  // Existing GraphCanvas far mode hides ordinary relations below zoom 0.38.
  assert.ok(fit >= 0.38, `dense Dagre fit ${fit} hides all ordinary relationships`)
  assert.ok(44*fit >= 10, 'all node bodies must remain individually observable')
  for (const [index, node] of nodes.entries()) for (const other of nodes.slice(index+1)) {
    assert.ok(Math.hypot(node.x-other.x,node.y-other.y) >= 44, 'dense layout must not overlap node bodies')
  }
  layout.destroy()
})
