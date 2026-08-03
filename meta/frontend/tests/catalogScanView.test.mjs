import assert from 'node:assert/strict'
import test from 'node:test'

import {
  isDirectLeafCatalog,
  selectLiveCatalogTopEntries,
  selectScannedCatalogTopEntries
} from '../src/utils/catalogScanView.js'

const directResource = { catalog_top_term: 'topic', catalog_leaf_term: 'topic' }
const branchResource = { catalog_top_term: 'schema', catalog_leaf_term: 'table' }

test('direct leaf catalog is derived from catalog terms', () => {
  assert.equal(isDirectLeafCatalog(directResource), true)
  assert.equal(isDirectLeafCatalog(branchResource), false)
  assert.equal(isDirectLeafCatalog({ catalog_leaf_term: 'topic' }), false)
})

test('live top entries select leaf or branch from the same catalog model rule', () => {
  const entries = [
    { name: 'public', role: 'branch' },
    { name: 'orders', role: 'leaf' }
  ]
  assert.deepEqual(selectLiveCatalogTopEntries(entries, directResource), [entries[1]])
  assert.deepEqual(selectLiveCatalogTopEntries(entries, branchResource), [entries[0]])
})

test('scanned direct leaves are read from root items while branch engines use child nodes', () => {
  const tree = {
    top_nodes: [{ id: 7, full_name: '' }],
    child_nodes: [{ id: 8, parent_node_id: 7, name: 'public', scan_status: 'completed' }],
    items: [{ id: 9, node_id: 7, name: 'orders', full_name: 'orders', item_type: 'topic', scanned_at: '2026-08-03T16:00:00Z' }]
  }

  assert.deepEqual(selectScannedCatalogTopEntries(tree, directResource), [{
    id: 9,
    name: 'orders',
    item_type: 'topic',
    node_type: 'topic',
    path: 'orders',
    role: 'leaf',
    scan_status: 'completed',
    scanned_depth: '',
    scanned_at: '2026-08-03T16:00:00Z',
    item_count: 1,
    total_size_bytes: 0
  }])
  assert.equal(selectScannedCatalogTopEntries(tree, branchResource)[0].name, 'public')
})
