import assert from 'node:assert/strict'
import { test } from 'vitest'
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
