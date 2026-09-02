import test from 'node:test'
import assert from 'node:assert/strict'

import { toQueryDevTaskPayload } from '../src/utils/queryTaskPayload.mjs'

test('saved relation query keeps an optional default table in the unified query parameter', () => {
  const payload = toQueryDevTaskPayload({
    name: 'person_metric',
    engine_id: 2,
	query: 'SELECT * FROM person',
    query_type: 'sql',
	query_parameters: [{
	  name: 'person', type: 'relation',
	  default: { locator: 'addp://engine/2/path/public/person?type=table' }
	}]
  })
	assert.deepEqual(payload.content.query_parameters, [{
	  name: 'person', type: 'relation',
	  default: { locator: 'addp://engine/2/path/public/person?type=table' }
	}])
	assert.equal('relation_inputs' in payload.content, false)
  assert.equal(payload.content.target_locator, undefined)
  assert.deepEqual(payload.execution_config, { engine_id: 2 })
})
