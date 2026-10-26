import assert from 'node:assert/strict'
import fs from 'node:fs'
import test from 'node:test'
import { prepareManualScan } from '../src/utils/manualScan.js'
import { createEngineCatalogTreeAdapter } from '../../../common-frontend/basic/src/utils/engineCatalogTree.mjs'
import { selectionFromResourceTreeNode } from '../../../common-frontend/basic/src/utils/resourceSelection.js'

const engine = { id: 2, name: 'Business PostgreSQL' }
const locator = 'addp://engine/2/path/Out.door/%20people.v2%20?type=table&item_id=10'
const selection = { identity: { locator, engine_id: 2 }, display: { path: 'Out.door /  people.v2 ' } }

test('a precise table scan defaults to basic and preserves the entire locator', () => {
  const payload = prepareManualScan({ engine, selection })
  assert.deepEqual(payload, {
    engine_id: 2, targets: [locator], scan_depth: 'basic', force: false,
    trigger_type: 'manual', source: 'meta'
  })
  assert.equal('catalog_paths' in payload, false)
  assert.equal('item_id' in payload, false)
  assert.equal(Object.isFrozen(payload), true)
  assert.equal(Object.isFrozen(payload.targets), true)
  selection.identity.locator = 'changed'
  assert.deepEqual(payload.targets, [locator])
  selection.identity.locator = locator
})

test('engine, catalog batch and precise selection use mutually exclusive selectors', () => {
  assert.deepEqual(prepareManualScan({ engine, scope: { kind: 'engine' } }), {
    engine_id: 2, scan_depth: 'basic', force: false, trigger_type: 'manual', source: 'meta'
  })
  const scope = { kind: 'catalog', paths: [' schema.with.dots ', 'other'] }
  const payload = prepareManualScan({ engine, scope, scanDepth: 'deep', force: true })
  assert.deepEqual(payload.catalog_paths, scope.paths)
  assert.equal(payload.scan_depth, 'deep')
  assert.equal(payload.force, true)
  scope.paths.push('outside')
  assert.equal(payload.catalog_paths.length, 2)
})

test('empty selection never silently becomes an engine scan', () => {
  for (const input of [
    {}, { scope: { kind: 'catalog', paths: [] } },
    { scope: { kind: 'catalog', paths: [' '] } },
    { scope: { kind: 'unknown' } },
    { selection: { identity: { locator: 'bad' } } },
    { selection: { identity: { locator: locator.replace('/2/', '/3/') } } },
    { selection, scope: { kind: 'engine' } },
    { selection, scanDepth: 'unknown' },
    { selection, force: 'true' }
  ]) assert.throws(() => prepareManualScan({ engine, ...input }))
  assert.throws(() => prepareManualScan({ engine: { id: 0 }, selection }))
})

test('a newly discovered native prefix and its leaf each submit only their exact locator', async () => {
  const engine = { id: 12, name: 'Business MinIO', engine_type: 'minio' }
  const segments = [{ term: 'service', kind: 'service', name: '' }]
  const make = (term, name, role, parent) => ({ name, term, kind: term, role,
    path: { version: 'catalog.path/v1', engine_id: 12, segments: [...parent, { term, kind: term, name }] } })
  const root = { name: '', term: 'service', kind: 'service', role: 'branch', path: { version: 'catalog.path/v1', engine_id: 12, segments } }
  const bucket = make('bucket', 'manager', 'branch', segments)
  const prefix = make('prefix', 'p65-gltf-20261026-8b7076d4', 'branch', bucket.path.segments)
  const leaf = make('object', 'model.gltf', 'leaf', prefix.path.segments)
  const adapter = createEngineCatalogTreeAdapter(engine, async (_, path) =>
    [[root], [bucket], [prefix], [leaf]][path.segments.length])
  const tree = await adapter.getTreeRoot()
  const [directory] = await adapter.getNodeChildren(tree.children[0])
  const [object] = await adapter.getNodeChildren(directory)
  for (const node of [directory, object]) {
    const selection = selectionFromResourceTreeNode(node, engine)
    assert.equal(selection.identity.node_id, undefined)
    assert.equal(selection.identity.item_id, undefined)
    const payload = prepareManualScan({ engine, selection })
    assert.deepEqual(payload.targets, [node.locator])
    assert.equal('catalog_paths' in payload, false)
    assert.equal(payload.force, false)
    assert.equal(payload.scan_depth, 'basic')
  }
  assert.equal(directory.locator, 'addp://engine/12/path/manager/p65-gltf-20261026-8b7076d4?type=prefix')
  assert.equal(object.locator, 'addp://engine/12/path/manager/p65-gltf-20261026-8b7076d4/model.gltf?type=object')
})

test('all interactive manual entry points open one dialog before the only submit path', () => {
  const view = fs.readFileSync(new URL('../src/views/MetadataScan.vue', import.meta.url), 'utf8')
  const dialog = fs.readFileSync(new URL('../src/components/ManualScanDialog.vue', import.meta.url), 'utf8')
  assert.equal((view.match(/metaApi\.createManualScanRun\(/g) || []).length, 1)
  assert.match(view, /<ManualScanDialog/)
  assert.match(view, /lifecycle_state: selectedResource.value.lifecycle_state/)
  assert.match(view, /connection_status: selectedResource.value.connection_status/)
  assert.match(dialog, /ResourceTreePicker/)
  assert.match(dialog, /ref\('basic'\)/)
  assert.match(dialog, /prepareManualScan/)
  assert.doesNotMatch(dialog, /createManualScanRun|fetch\(|axios/)
  for (const language of ['zh-cn', 'en']) {
    const words = JSON.parse(fs.readFileSync(new URL(`../src/i18n/${language}.json`, import.meta.url), 'utf8')).meta.scan.manual
    for (const key of ['title', 'open', 'confirm', 'target', 'basicHint', 'deepHint', 'invalidTarget', 'submitted', 'completed']) {
      assert.equal(typeof words[key], 'string', `${language}: ${key}`)
    }
  }
})
