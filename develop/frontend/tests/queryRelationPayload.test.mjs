import test from 'node:test'
import assert from 'node:assert/strict'

import { toQueryDevTaskPayload } from '../src/utils/queryTaskPayload.mjs'

test('saved relation query contains aliases but no physical input locator', () => {
  const payload = toQueryDevTaskPayload({
    name: 'person_metric',
    engine_id: 2,
    query: 'SELECT * FROM addp_input.person',
    query_type: 'sql',
    relation_inputs: ['person'],
    query_parameters: []
  })
  assert.deepEqual(payload.content.relation_inputs, ['person'])
  assert.equal(payload.content.target_locator, undefined)
  assert.deepEqual(payload.execution_config, { engine_id: 2 })
})
