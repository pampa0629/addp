import assert from 'node:assert/strict'
import test from 'node:test'

import {
  addExpandedKey,
  defaultExpandedKeys,
  hasExpandableChildren,
  removeExpandedKey,
  resolveExpandedKeys
} from '../src/utils/resourceTreeState.mjs'

const roots = [
  {
    id: 'engine-1',
    children: [
      { id: 'schema-public', children: [{ id: 'table-users' }] }
    ]
  },
  { id: 'engine-2', children: [] }
]

test('defaultExpandedKeys expands every root without expanding descendants', () => {
  assert.deepEqual(defaultExpandedKeys(roots, { expandRoot: true }), ['engine-1', 'engine-2'])
})

test('defaultExpandedKeys expands all loaded descendants when requested', () => {
  assert.deepEqual(
    defaultExpandedKeys(roots, { expandAll: true }),
    ['engine-1', 'schema-public', 'table-users', 'engine-2']
  )
})

test('expanding a schema preserves all currently expanded engine roots', () => {
  const currentKeys = resolveExpandedKeys({
    override: null,
    expandedKeys: [],
    treeData: roots,
    expandRoot: true
  })
  assert.deepEqual(
    addExpandedKey(currentKeys, 'schema-public'),
    ['engine-1', 'engine-2', 'schema-public']
  )
})

test('expansion transitions are idempotent and support a fully collapsed tree', () => {
  assert.deepEqual(addExpandedKey(['engine-1'], 'engine-1'), ['engine-1'])
  assert.deepEqual(removeExpandedKey(['engine-1'], 'engine-1'), [])
  assert.deepEqual(resolveExpandedKeys({
    override: [],
    expandedKeys: [],
    treeData: roots,
    expandRoot: true
  }), [])
})

test('hasExpandableChildren accepts every resource-tree contract shape', () => {
  assert.equal(hasExpandableChildren({ hasChildren: true }), true)
  assert.equal(hasExpandableChildren({ metadata: { has_children: true } }), true)
  assert.equal(hasExpandableChildren({ metadata: { item_count: 2 } }), true)
  assert.equal(hasExpandableChildren({ children: [{ id: 'child' }] }), true)
  assert.equal(hasExpandableChildren({ hasChildren: false, children: [] }), false)
})
