import test from 'node:test'
import assert from 'node:assert/strict'

import {
  isNativeTargetSelectable,
  sameNativeTargetParentIdentity
} from '../src/views/TaskWizard/nativeTargetSelection.mjs'

test('native target picker accepts parent nodes and existing table items', () => {
  assert.equal(isNativeTargetSelectable({ type: 'database' }, { locator: { nodeId: 1 } }), true)
  assert.equal(isNativeTargetSelectable({ type: 'schema' }, { locator: { nodeId: 2 } }), true)
  assert.equal(isNativeTargetSelectable({ type: 'table' }, { locator: { itemId: 3 } }), true)
})

test('native target picker rejects table nodes without an item identity', () => {
  assert.equal(isNativeTargetSelectable({ type: 'table' }, { locator: { nodeId: 3 } }), false)
  assert.equal(isNativeTargetSelectable({ type: 'directory' }, { locator: { nodeId: 4 } }), false)
})

test('restoring the same native target parent keeps existing field mappings', () => {
  const restored = { engineID: 3, type: 'schema', path: ['business'] }
  const selected = { engineID: 3, type: 'schema', path: ['business'] }
  const changed = { engineID: 3, type: 'schema', path: ['archive'] }

  assert.equal(sameNativeTargetParentIdentity(restored, selected), true)
  assert.equal(sameNativeTargetParentIdentity(restored, changed), false)
})
