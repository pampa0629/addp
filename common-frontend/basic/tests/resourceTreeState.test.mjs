import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
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
  assert.equal(hasExpandableChildren({ metadata: { item_count: 2 } }), false)
  assert.equal(hasExpandableChildren({ children: [{ id: 'child' }] }), true)
  assert.equal(hasExpandableChildren({ hasChildren: false, children: [] }), false)
})

test('item counts do not turn resource-tree leaves into branches', () => {
  for (const type of ['table', 'view', 'collection', 'object']) {
    assert.equal(hasExpandableChildren({
      type, hasChildren: false, children: [],
      metadata: { item_id: 42, item_count: 1, has_children: false }
    }), false)
  }
  assert.equal(hasExpandableChildren({
    type: 'schema', hasChildren: true, children: [],
    metadata: { item_count: 0 }
  }), true)
})

test('resource tree does not model an engine as a resource node type', () => {
  const resourceTree = readFileSync(new URL('../src/components/ResourceTree.vue', import.meta.url), 'utf8')

  assert.doesNotMatch(resourceTree, /data\.type\s*===\s*['"]engine['"]/)
  assert.doesNotMatch(resourceTree, /resolveEngineIcon|getEngineIconName/)
})

test('Manager reuses the shared expansion predicate without an item-count fallback', () => {
  const store = readFileSync(new URL('../../../manager/frontend/src/stores/explorer.js', import.meta.url), 'utf8')
  assert.match(store, /import \{ hasExpandableChildren \} from '@common-ui\/utils\/resourceTreeState\.mjs'/)
  assert.match(store, /tree\.hasChildren = hasExpandableChildren\(tree\)/)
  assert.doesNotMatch(store, /function hasTreeNodeChildren|Number\(node\.metadata\?\.item_count/)
})
