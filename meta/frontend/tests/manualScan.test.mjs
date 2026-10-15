import assert from 'node:assert/strict'
import fs from 'node:fs'
import test from 'node:test'
import { prepareManualScan } from '../src/utils/manualScan.js'

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

test('all interactive manual entry points open one dialog before the only submit path', () => {
  const view = fs.readFileSync(new URL('../src/views/MetadataScan.vue', import.meta.url), 'utf8')
  const dialog = fs.readFileSync(new URL('../src/components/ManualScanDialog.vue', import.meta.url), 'utf8')
  assert.equal((view.match(/metaApi\.createManualScanRun\(/g) || []).length, 1)
  assert.match(view, /<ManualScanDialog/)
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
