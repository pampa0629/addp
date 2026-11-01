import assert from 'node:assert/strict'
import test from 'node:test'

import { formatLocatorDisplayPath, getParentLocator, parseLocator } from '../src/types/resourceLocator.js'
import { collectSelectableResources, resourceSelectionState, updateResourceSelection, geometryColumnFactsFromSelection, selectionFromResourceTreeNode, toggleResourceSelection } from '../src/utils/resourceSelection.js'

test('multiple resource selection preserves click order and uses locator identity across tree and search', () => {
  const first = { identity: { locator: 'addp://engine/1/path/a.tif?type=object&item_id=1' } }
  const second = { identity: { locator: 'addp://engine/2/path/b.tif?type=object&item_id=2' } }
  const initial = [first]
  const both = toggleResourceSelection(initial, second)
  assert.deepEqual(initial, [first])
  assert.deepEqual(both, [first, second])
  const removed = toggleResourceSelection(both, { ...first, display: { label: 'Search result' } })
  assert.deepEqual(removed, [second])
  assert.deepEqual(toggleResourceSelection(removed, first), [second, first])
  assert.deepEqual(toggleResourceSelection(removed, null), [second])
})

test('resource selection carries engine identity, native database path, and spatial facts', () => {
  const selection = selectionFromResourceTreeNode({
    locator: 'addp://engine/2/path/public/farmland?type=table&item_id=51572',
    label: 'farmland',
    type: 'table',
    metadata: {
      spatial: {
        geometry_columns: ['geom', 'center'],
        primary_geometry_column: 'geom'
      }
    }
  }, { id: 2, name: '业务 PostgreSQL', engine_type: 'postgresql' })

  assert.equal(selection.display.engine_name, '业务 PostgreSQL')
  assert.equal(selection.display.path, 'public.farmland')
  assert.deepEqual(selection.resource.spatial, {
    geometry_columns: ['geom', 'center'],
    primary_geometry_column: 'geom'
  })
  assert.deepEqual(geometryColumnFactsFromSelection(selection), {
    columns: ['geom', 'center'],
    selected: 'geom'
  })
})

test('native path formatting preserves slash semantics for object and file engines', () => {
  assert.equal(
    formatLocatorDisplayPath('addp://engine/3/path/results/vector/buffer.gpkg?type=object', { engineType: 'minio' }),
    'results/vector/buffer.gpkg'
  )
  assert.equal(
    formatLocatorDisplayPath('addp://engine/4/path/gis/roads.shp?type=file', { engineType: 'nfs' }),
    'gis/roads.shp'
  )
})

test('index selection preserves dotted native names and returns its service root', () => {
  const uri = 'addp://engine/14/path/addp_orders.v1?type=index&item_id=15'
  assert.equal(formatLocatorDisplayPath(uri), 'addp_orders.v1')
  assert.deepEqual(getParentLocator(parseLocator(uri)), {
    engineId: 14, path: [], type: 'service'
  })
})

test('resource spatial facts do not infer undeclared geometry field names', () => {
  const selection = selectionFromResourceTreeNode({
    locator: 'addp://engine/2/path/public/plain_table?type=table&item_id=51573',
    label: 'plain_table',
    type: 'table',
    metadata: { spatial: { srid: 4326 } }
  }, { id: 2, name: '业务 PostgreSQL', engine_type: 'postgresql' })

  assert.deepEqual(geometryColumnFactsFromSelection(selection), { columns: [], selected: '' })
})

test('display preserves whitespace-only physical segments and filename padding', () => {
  assert.equal(formatLocatorDisplayPath('addp://engine/33/path/%20/%20TEST.PNG%20?type=file'), ' / TEST.PNG ')
})

// Branch selection uses the complete snapshot; callers never pass filtered clones.
test('branch selection stops at eligible containers, distinguishes direct children, and reports partial state', () => {
  const a = { locator: 'a', eligible: true, children: [{ locator: 'internal', eligible: true }] }
  const b = { locator: 'b', eligible: true }
  const directory = { children: [a, { children: [b] }] }
  const eligible = node => !!node.eligible
  assert.deepEqual(collectSelectableResources(directory, eligible), [a])
  assert.deepEqual(collectSelectableResources(directory, eligible, true), [a, b])
  assert.deepEqual(collectSelectableResources(a, eligible, true), [a])
  const selected = [{ identity: { locator: 'a' } }]
  assert.deepEqual(resourceSelectionState(selected, [a, b]), { checked: false, indeterminate: true })
  assert.deepEqual(resourceSelectionState(selected, [a]), { checked: true, indeterminate: false })
  assert.deepEqual(resourceSelectionState(selected, []), { checked: false, indeterminate: false })
})

test('batch selection deduplicates by locator, preserves order and rejects overflow without mutating the original', () => {
  const a = { identity: { locator: 'a' } }, b = { identity: { locator: 'b' } }, c = { identity: { locator: 'c' } }
  const selected = [a]
  assert.deepEqual(updateResourceSelection(selected, [a, b], true, 2), [a, b])
  assert.throws(() => updateResourceSelection(selected, [b, c], true, 2), /resourceSelectionLimit/)
  assert.deepEqual(selected, [a])
  assert.deepEqual(updateResourceSelection([a, b], [a], false, 2), [b])
  assert.throws(() => updateResourceSelection(selected, [{}], true), /invalidResourceSelection/)
})
