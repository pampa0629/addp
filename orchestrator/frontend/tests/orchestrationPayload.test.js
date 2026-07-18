import assert from 'node:assert/strict'
import test from 'node:test'

import { buildOrchestrationPayload } from '../src/utils/orchestrationPayload.js'

test('orchestration writes include only user-editable fields', () => {
  assert.deepEqual(buildOrchestrationPayload({
    id: 42,
    tenant_id: 99,
    name: 'daily',
    description: 'sync data',
    steps: [{ id: 'scan' }],
    enabled: true,
    schedule: '0 2 * * *',
    created_by: 7,
    last_execution_status: 'success'
  }), {
    name: 'daily',
    description: 'sync data',
    steps: [{ id: 'scan' }],
    enabled: true,
    schedule: '0 2 * * *'
  })
})
