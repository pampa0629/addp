import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import { collectionTaskPayload, isEngineCollectionTask, saveCollectionTask, cancelCollectionTaskRun } from '../src/utils/collectionTask.js'

const engine = { id: 2, resource_type: 'postgresql' }
const form = { name: ' Basic collection ', scheduled: false, schedule: '0 * * * *' }
const task = { id: 42, engine_id: 2, scope: { type: 'engine', engine_id: 2 }, parameters: { scan_depth: 'basic', force: false } }

test('manual PG basic definition has explicit entire-engine scope and no cron or immediate execution', () => {
  assert.deepEqual(collectionTaskPayload(engine, form), {
    name: 'Basic collection', description: '', scope: { type: 'engine', engine_id: 2 },
    scan_depth: 'basic', force: false, schedule: '', enabled: false
  })
})

test('saving creates or updates only the definition, never approves or triggers a scan', async () => {
  const calls = []
  const api = new Proxy({
    createScanTask: async (...args) => { calls.push(['create', ...args]); return task },
    updateScanTask: async (...args) => { calls.push(['update', ...args]); return task }
  }, { get: (object, name) => { assert.ok(name in object, `unexpected API: ${name}`); return object[name] } })
  assert.equal(await saveCollectionTask(api, engine, form, null), task)
  assert.equal(await saveCollectionTask(api, engine, form, task), task)
  assert.deepEqual(calls.map(call => call.slice(0, -1)), [['create', 2], ['update', 2, 42]])
  assert.ok(calls.every(call => call.at(-1).enabled === false && call.at(-1).schedule === ''))
})

test('scope or depth is never silently converted; other engines stay outside this editor', () => {
  for (const invalid of [{ ...engine, resource_type: 'nfs' }, { ...engine, id: 0 }]) {
    assert.throws(() => collectionTaskPayload(invalid, form))
  }
  for (const invalid of [
    { ...task, engine_id: 3 }, { ...task, scope: { type: 'catalog_path' } },
    { ...task, parameters: { scan_depth: 'deep' } }
  ]) assert.throws(() => collectionTaskPayload(engine, form, invalid))
  assert.equal(isEngineCollectionTask(task, '2'), true)
  assert.throws(() => collectionTaskPayload(engine, { ...form, name: ' ' }))
  assert.throws(() => collectionTaskPayload(engine, { ...form, scheduled: true, schedule: '' }))
  assert.equal(collectionTaskPayload(engine, { ...form, scheduled: true }).schedule, '0 * * * *')
})

test('PG task editor and task trigger replace the PG schedule shortcut without borrowing personal scan', () => {
  const view = fs.readFileSync(new URL('../src/views/MetadataScan.vue', import.meta.url), 'utf8')
  const dialog = fs.readFileSync(new URL('../src/components/CollectionTaskDialog.vue', import.meta.url), 'utf8')
  assert.match(view, /row.resource_type === 'postgresql'[\s\S]*?collectionDialogVisible.value = true[\s\S]*?return/)
  const execute = view.slice(view.indexOf('const executeCollectionTask ='), view.indexOf('const restoreRouteState ='))
  assert.match(execute, /metaApi.triggerScanTask\(task.id\)/)
  assert.doesNotMatch(execute, /createManualScanRun|createScanTask/)
  assert.match(dialog, /dirty \|\| unsupported/)
  assert.match(dialog, /ScheduleConfig/)
  assert.doesNotMatch(dialog, /openApproval|collection-qualifications|meta_collection_qualification/)
  assert.match(dialog, /scheduled: false/)
  for (const lang of ['zh-cn', 'en']) {
    const words = JSON.parse(fs.readFileSync(new URL(`../src/i18n/${lang}.json`, import.meta.url))).meta.scan.collection
    for (const key of ['title', 'scope', 'save', 'saved', 'managementHint', 'execute', 'invalid', 'unsupported']) assert.ok(words[key])
  }
})


test('cancellation targets only the existing PG basic execution and never changes its schedule', async () => {
  const calls = []
  const run = { execution_id: 'a4a910ec-e39f-4ac1-8a53-582bd6da9a96', status: 'running' }
  const active = { ...task, last_execution_id: run.execution_id, last_execution_status: 'running', enabled: true, schedule: '0 * * * *' }
  const before = structuredClone(active)
  const api = new Proxy({ cancelScanExecution: async id => { calls.push(id); return run } }, {
    get: (object, name) => { assert.ok(name in object, `unexpected API: ${name}`); return object[name] }
  })
  assert.equal(await cancelCollectionTaskRun(api, engine, active), run)
  assert.deepEqual(calls, [run.execution_id])
  assert.deepEqual(active, before)
  for (const invalid of [{ ...active, engine_id: 3 }, { ...active, parameters: { scan_depth: 'deep' } },
    { ...active, last_execution_status: 'success' }, { ...active, last_execution_id: '' }]) {
    await assert.rejects(cancelCollectionTaskRun(api, engine, invalid))
  }
  await assert.rejects(cancelCollectionTaskRun(api, { ...engine, resource_type: 'nfs' }, active))
  assert.equal(calls.length, 1)
})
