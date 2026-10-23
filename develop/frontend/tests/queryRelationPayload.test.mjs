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

 test('query task saves only explicit masking references and detaches the draft', () => {
 const choices=[{target:{owner_module:'meta',resource_type:'data_item',resource_identity:'one'},component_key:'phone',selection:{policy_id:'policy',assessment_revision:1,policy_revision:2,option_id:'hash'}}]
 const payload=toQueryDevTaskPayload({name:'mask choice',engine_id:2,query:'SELECT phone FROM person',protection_choices:choices})
 assert.deepEqual(payload.content.protection_choices,choices)
 payload.content.protection_choices[0].selection.option_id='other'
 assert.equal(choices[0].selection.option_id,'hash')
 assert.deepEqual(toQueryDevTaskPayload({}).content.protection_choices,[])
 })

test('query and export methods are independent saved snapshots', () => {
  const source = { target: { owner_module: 'meta', resource_type: 'data_item', resource_identity: 'one' }, component_key: 'phone' }
  const query = [{ ...source, selection: { policy_id: 'query', option_id: 'mask', policy_revision: 1, assessment_revision: 1 } }]
  const exported = [{ ...source, selection: { policy_id: 'export', option_id: 'hash', policy_revision: 2, assessment_revision: 1 } }]
  const saved = toQueryDevTaskPayload({ query: 'SELECT phone FROM person', protection_choices: query, export_protection_choices: exported })
  assert.deepEqual(saved.content.protection_choices, query)
  assert.deepEqual(saved.content.export_protection_choices, exported)
  saved.content.export_protection_choices[0].selection.option_id = 'changed'
  assert.equal(exported[0].selection.option_id, 'hash')
  assert.equal(query[0].selection.option_id, 'mask')
  assert.deepEqual(toQueryDevTaskPayload({ protection_choices: query }).content.export_protection_choices, [])
})
