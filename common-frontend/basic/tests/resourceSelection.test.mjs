import assert from 'node:assert/strict'
import test from 'node:test'

import { formatLocatorDisplayPath } from '../src/types/resourceLocator.js'
import { geometryColumnFactsFromSelection, selectionFromResourceTreeNode } from '../src/utils/resourceSelection.js'

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

test('resource spatial facts do not infer undeclared geometry field names', () => {
  const selection = selectionFromResourceTreeNode({
    locator: 'addp://engine/2/path/public/plain_table?type=table&item_id=51573',
    label: 'plain_table',
    type: 'table',
    metadata: { spatial: { srid: 4326 } }
  }, { id: 2, name: '业务 PostgreSQL', engine_type: 'postgresql' })

  assert.deepEqual(geometryColumnFactsFromSelection(selection), { columns: [], selected: '' })
})
