import test from 'node:test'
import assert from 'node:assert/strict'
import { buildSchemaChangeApproval, buildSchemaChangeScanRetry, getSchemaChangeScanNotice } from '../src/utils/schemaChange.mjs'

test('builds an explicit additive schema approval', () => {
  assert.deepEqual(buildSchemaChangeApproval([
    { source: ' added ', target: ' renamed ', target_type: 'string', nullable: true }
  ]), {
    fields: [{ source: 'added', target: 'renamed', target_type: 'string', nullable: true }]
  })
})

test('rejects incomplete, non-nullable, and duplicate target mappings', () => {
  assert.equal(buildSchemaChangeApproval([]), null)
  assert.equal(buildSchemaChangeApproval([
    { source: 'a', target: 'a', target_type: 'string', nullable: false }
  ]), null)
  assert.equal(buildSchemaChangeApproval([
    { source: 'a', target: 'same', target_type: 'string', nullable: true },
    { source: 'b', target: 'same', target_type: 'string', nullable: true }
  ]), null)
})

test('classifies persisted Meta scan claims without treating failed as retryable', () => {
  const now = Date.parse('2026-07-25T06:00:00Z')
  assert.deepEqual(getSchemaChangeScanNotice({ status: 'applied', metadata_scan_status: 'pending' }, now), {
    state: 'pending', attempt: 0, retryable: true
  })
  assert.deepEqual(getSchemaChangeScanNotice({
    status: 'applied', metadata_scan_status: 'running', metadata_scan_attempt: 1,
    metadata_scan_lease_until: '2026-07-25T06:01:00Z'
  }, now), { state: 'running', attempt: 1, retryable: false })
  assert.deepEqual(getSchemaChangeScanNotice({
    status: 'applied', metadata_scan_status: 'running', metadata_scan_attempt: 2,
    metadata_scan_lease_until: '2026-07-25T05:59:00Z'
  }, now), { state: 'expired', attempt: 2, retryable: true })
  assert.deepEqual(getSchemaChangeScanNotice({
    status: 'applied', metadata_scan_status: 'failed', metadata_scan_attempt: 1
  }, now), { state: 'failed', attempt: 1, retryable: false })
  assert.equal(getSchemaChangeScanNotice({ status: 'applied', metadata_scan_status: 'success' }, now), null)
})

test('retries only pending or expired Meta scan claims with approved mappings', () => {
  const request = {
    status: 'applied', metadata_scan_status: 'pending',
    approved_mappings: [{ source: 'added', target: 'renamed', target_type: 'string', nullable: true }]
  }
  assert.deepEqual(buildSchemaChangeScanRetry(request), {
    fields: [{ source: 'added', target: 'renamed', target_type: 'string', nullable: true }]
  })
  assert.equal(buildSchemaChangeScanRetry({ ...request, metadata_scan_status: 'failed' }), null)
})
