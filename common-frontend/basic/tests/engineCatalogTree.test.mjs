import assert from 'node:assert/strict'
import fs from 'node:fs'
import test from 'node:test'
import { createEngineCatalogTreeAdapter, safePickerEngineID } from '../src/utils/engineCatalogTree.mjs'

const engine = { id: 12, name: 'Business MinIO', engine_type: 'minio' }
const entry = (segments, role) => {
  const last = segments.at(-1)
  return { ...last, role, path: { version: 'catalog.path/v1', engine_id: 12, segments } }
}
const root = entry([{ term: 'service', kind: 'service', name: '' }], 'branch')
const bucket = entry([...root.path.segments, { term: 'bucket', kind: 'bucket', name: 'manager' }], 'branch')
const prefix = entry([...bucket.path.segments, { term: 'prefix', kind: 'prefix', name: ' new %20目录 ' }], 'branch')
const object = entry([...prefix.path.segments, { term: 'object', kind: 'object', name: ' model.gltf ' }], 'leaf')

function catalog() {
  const calls = []
  const list = async (id, path) => {
    calls.push({ id, path })
    return [[root], [bucket], [prefix], [object], []][path.segments.length]
  }
  return { calls, adapter: createEngineCatalogTreeAdapter(engine, list) }
}

test('native browsing preserves an unscanned prefix and leaf, without metadata IDs or parent substitution', async () => {
  const { calls, adapter } = catalog()
  const tree = await adapter.getTreeRoot()
  const [directory] = await adapter.getNodeChildren(tree.children[0])
  const [leaf] = await adapter.getNodeChildren(directory)
  assert.equal(directory.locator, 'addp://engine/12/path/manager/%20new%20%2520%E7%9B%AE%E5%BD%95%20?type=prefix')
  assert.equal(leaf.locator, `${directory.locator.split('?')[0]}/%20model.gltf%20?type=object`)
  assert.equal(directory.hasChildren, true)
  assert.equal(leaf.hasChildren, false)
  assert.deepEqual(leaf.metadata.catalog_entry, object)
  assert.equal(leaf.metadata.engine_type, 'minio')
  assert.deepEqual(calls.map(call => call.path), [{ segments: [] }, root.path, bucket.path, prefix.path])
  assert.ok(calls.every(call => call.id === '12'))
  assert.throws(() => adapter.getNodeChildren(leaf), /invalidCatalogTarget/)
  assert.equal(calls.length, 4)
  leaf.metadata.catalog_entry.path.segments.at(-1).name = 'mutated'
  assert.equal(object.path.segments.at(-1).name, ' model.gltf ')
})

test('searches native paths with caller-owned eligibility and rejects partial directory failures', async () => {
  const { adapter } = catalog()
  assert.deepEqual((await adapter.search(12, 'new')).map(node => node.type), ['prefix', 'object'])
  const leafOnly = createEngineCatalogTreeAdapter(engine, async (_, path) =>
    [[root], [bucket], [prefix], [object]][path.segments.length], { isSearchable: node => !node.hasChildren })
  assert.deepEqual((await leafOnly.search(12, 'new')).map(node => node.type), ['object'])
  const broken = createEngineCatalogTreeAdapter(engine, async (_, path) => {
    if (path.segments.length === 3) throw new Error('directory unavailable')
    return [[root], [bucket], [prefix]][path.segments.length]
  })
  await assert.rejects(broken.search(12, 'missing'), /directory unavailable/)
  await assert.rejects(adapter.search(13, 'new'), /invalidCatalogTarget/)
  await assert.rejects(adapter.search(12, 'new', { limit: 0 }), /invalidCatalogTarget/)
})

test('invalid IDs, foreign facts, malformed responses and oversized searches never fall back to another source', async () => {
  for (const id of ['9007199254740993', '01', '1x', 0]) assert.throws(() => safePickerEngineID(id))
  for (const response of [null, { nodes: [root] }, [], [root, root], [{ ...root, role: 'leaf' }],
    [{ ...root, path: { ...root.path, engine_id: 13 } }]]) {
    const adapter = createEngineCatalogTreeAdapter(engine, async () => response)
    await assert.rejects(adapter.getTreeRoot(), /invalidCatalogTarget/)
  }
  const large = createEngineCatalogTreeAdapter(engine, async (_, path) =>
    path.segments.length === 0 ? [root] : Array.from({ length: 2001 }, () => bucket))
  await assert.rejects(large.search(12, 'absent'), /invalidCatalogTarget/)
})

test('System approval and Meta scanning share the native adapter and keep business policy outside it', () => {
  const system = fs.readFileSync(new URL('../../../system/frontend/src/utils/engineApprovalCatalog.js', import.meta.url), 'utf8')
  const meta = fs.readFileSync(new URL('../../../meta/frontend/src/components/ManualScanDialog.vue', import.meta.url), 'utf8')
  assert.match(system, /createEngineCatalogTreeAdapter\(engine,/)
  assert.match(system, /isSearchable: isApprovalSelection/)
  assert.doesNotMatch(system, /buildLocator|searchSequence|function safePickerEngineID/)
  assert.match(meta, /:adapter="catalogAdapter"/)
  assert.match(meta, /createEngineCatalogTreeAdapter\(props.context.engine,/)
  assert.doesNotMatch(meta, /getResourceTree|searchResourceTree|api-base-url/)
})
