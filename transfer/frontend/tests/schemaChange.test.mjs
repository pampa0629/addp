import test from 'node:test'
import assert from 'node:assert/strict'
import { buildSchemaChangeApproval } from '../src/utils/schemaChange.mjs'

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
