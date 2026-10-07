import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'
import { serializeEngineApprovalInitialization } from '../src/utils/engineApprovalInitialization.mjs'

const target = { engine_id: '9007199254740993', version: 'v1', segments: [{ term: 'table', kind: 'table', name: 'a"b/中文' }] }
test('both explicit modes use one lossless numeric path serializer', () => {
  for (const mode of ['catalog', 'independent']) {
    const body = serializeEngineApprovalInitialization(target, mode, ' Reason "quoted" ')
    assert.match(body, /"engine_id":9007199254740993,/)
    assert.doesNotMatch(body, /9007199254740992/)
    assert.equal(JSON.parse(body).mode, mode)
    assert.equal(JSON.parse(body).reason, 'Reason "quoted"')
    assert.deepEqual(JSON.parse(body).catalog_path.segments, target.segments)
    assert.deepEqual(Object.keys(JSON.parse(body)), ['catalog_path', 'mode', 'reason'])
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
  assert.match(panel, /ResourceTreePicker/)
  assert.doesNotMatch(panel, /<el-tree\b|<ResourceTree\s|\/meta\//)
  assert.match(panel, /serializeEngineApprovalInitialization/)
  assert.match(read('catalog/frontend/src/components/SharingPanel.vue'), /serializeEngineApprovalInitialization/)
  assert.doesNotMatch(read('catalog/frontend/src/utils/sharingConfirmation.js'), /serializeApprovalInitialization/)
})
