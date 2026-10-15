import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'
import { serializeEngineApprovalInitialization, serializeEngineCatalogTarget, readNativeApprovalFacts, observeNativeApprovalBinding } from '../src/utils/engineApprovalInitialization.mjs'

const target = { engine_id: '9007199254740993', version: 'v1', segments: [{ term: 'table', kind: 'table', name: 'a"b/中文' }] }
const binding = { item_fingerprint: 'a'.repeat(64), boundary_hash: 'b'.repeat(64), native_target: target }
test('direct grants reuse the same structured target serializer', () => {
  assert.match(serializeEngineCatalogTarget(target), /"engine_id":9007199254740993,/)
  const root = resolve(import.meta.dirname, '../../..')
  const grant = readFileSync(resolve(root, 'system/frontend/src/utils/independentGrant.js'), 'utf8')
  assert.match(grant, /serializeEngineCatalogTarget.*from '@common-ui'/)
  assert.doesNotMatch(grant, /Number\(.*engine_id/)
})
test('both explicit modes use one lossless numeric path serializer', () => {
  for (const mode of ['catalog', 'independent']) {
    const body = serializeEngineApprovalInitialization(target, mode, ' Reason "quoted" ', binding)
    assert.match(body, /"engine_id":9007199254740993,/)
    assert.doesNotMatch(body, /9007199254740992/)
    assert.equal(JSON.parse(body).mode, mode)
    assert.equal(JSON.parse(body).reason, 'Reason "quoted"')
    assert.deepEqual(JSON.parse(body).catalog_path.segments, target.segments)
    assert.deepEqual(Object.keys(JSON.parse(body)), ['catalog_path', 'mode', 'reason', 'item_fingerprint', 'expected_boundary_hash'])
  }
})
test('missing mode, malformed IDs, oversized int64 and empty reasons never serialize', () => {
  for (const id of [9007199254740993, '', '0', '01', '-1', '1e3', '1,"mode":"independent"', '9223372036854775808']) {
    assert.throws(() => serializeEngineApprovalInitialization({ ...target, engine_id: id }, 'independent', 'Reason'))
  }
  for (const mode of ['', undefined, 'unknown']) assert.throws(() => serializeEngineApprovalInitialization(target, mode, 'Reason'))
  for (const reason of ['', ' ', '字'.repeat(2001)]) assert.throws(() => serializeEngineApprovalInitialization(target, 'catalog', reason))
  assert.throws(() => serializeEngineApprovalInitialization(null, 'catalog', 'Reason'))
})
test('System and Catalog consume the same initializer and shared tree owner', () => {
  const root = resolve(import.meta.dirname, '../../..')
  const read = path => readFileSync(resolve(root, path), 'utf8')
  assert.match(read('common-frontend/basic/src/index.js'), /engineApprovalInitialization.mjs/)
  const panel = read('system/frontend/src/components/engines/EngineDataAuthorization.vue')
  assert.match(panel, /ResourceTreeSelect/)
  assert.doesNotMatch(panel, /<el-tree\b|<ResourceTree\s|\/meta\//)
  assert.match(panel, /serializeEngineApprovalInitialization/)
  assert.match(read('catalog/frontend/src/components/SharingPanel.vue'), /serializeEngineApprovalInitialization/)
  assert.doesNotMatch(read('catalog/frontend/src/utils/sharingConfirmation.js'), /serializeApprovalInitialization/)
})

test('native approval observation accepts exact numeric IDs and requires the trusted complete typed boundary', async () => {
  const native = { engine_id: 2, version: 'catalog.path/v1', segments: [
    { term: 'server', kind: 'server', name: '' }, { term: 'schema', kind: 'namespace', name: 'a.b' },
    { term: 'table', kind: 'table', name: 'c' }
  ] }
  const read = async (id, fingerprint) => {
    assert.equal(id, '2')
    assert.match(fingerprint, /^[0-9a-f]{64}$/)
    return { schema_version: 'meta.data_item_authorization_facts/v1', item_fingerprint: fingerprint, basis_state: 'complete',
      boundary_hash: 'b'.repeat(64), boundary: { native_target: native } }
  }
  const observed = await observeNativeApprovalBinding(native, read)
  assert.equal(Object.isFrozen(observed), true)
  assert.equal(observed.boundary_hash, 'b'.repeat(64))
  await assert.rejects(observeNativeApprovalBinding(native, async (...args) => ({ ...await read(...args), basis_state: 'incomplete' })))
  const collision = { ...native, segments: [native.segments[0], { ...native.segments[1], name: 'a' }, { ...native.segments[2], name: 'b.c' }] }
  await assert.rejects(observeNativeApprovalBinding(native, async (...args) => ({ ...await read(...args), boundary: { native_target: collision } })))
  await assert.rejects(observeNativeApprovalBinding({ ...native, engine_id: 9007199254740992 }, () => { throw new Error('rounded identity reached owner') }))
  assert.throws(() => serializeEngineApprovalInitialization({ ...native, engine_id: '2' }, 'independent', 'Explicit', undefined))
  for (const state of ['incomplete', 'needs_refresh', 'unsupported']) {
    const readState = async (...args) => ({ ...await read(...args), basis_state: state, boundary: null, boundary_hash: null })
    assert.deepEqual(await readNativeApprovalFacts(native, readState), { state, binding: null })
    await assert.rejects(observeNativeApprovalBinding(native, readState))
  }
  for (const basis_state of ['', 'unknown']) await assert.rejects(readNativeApprovalFacts(native, async (...args) => ({ ...await read(...args), basis_state })))
  await assert.rejects(readNativeApprovalFacts(native, async () => { throw new Error('dependency unavailable') }), /dependency unavailable/)
})
