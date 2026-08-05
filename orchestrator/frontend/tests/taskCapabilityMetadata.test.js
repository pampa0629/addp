import test from 'node:test'
import assert from 'node:assert/strict'
import { activeTaskCapabilityMetadata } from '../src/utils/taskCapabilityMetadata.js'

test('activeTaskCapabilityMetadata only exposes active type metadata', () => {
  assert.deepEqual(activeTaskCapabilityMetadata([
    { type: 'workflow', edit_url: '/develop/workflow/:id', deprecated: false },
    { type: 'legacy', edit_url: '/legacy/:id', deprecated: true },
  ]), [{ type: 'workflow', editUrl: '/develop/workflow/:id' }])
})
