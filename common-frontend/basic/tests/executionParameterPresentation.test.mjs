import assert from 'node:assert/strict'
import test from 'node:test'
import { summarizeExecutionResource } from '../src/utils/executionParameterPresentation.js'

test('resource summaries preserve scalar and object input contracts', () => {
  const locator = 'addp://engine/2/path/public/result?type=table'
  const engines = { 2: { name: 'Warehouse', engine_type: 'postgresql' } }
  const scalar = { schema: { type: 'string' } }
  const object = { schema: { type: 'object' } }
  const expected = {
    status: 'resolved', engineId: 2, engineName: 'Warehouse', name: 'public.result', type: 'table'
  }
  assert.deepEqual(summarizeExecutionResource(scalar, locator, engines), expected)
  assert.deepEqual(summarizeExecutionResource(object, { locator }, engines), expected)
  assert.equal(summarizeExecutionResource(scalar, { locator }, engines).status, 'empty')
  assert.equal(summarizeExecutionResource(object, locator, engines).status, 'empty')
  assert.equal(summarizeExecutionResource(scalar, '{{source.outputs.target_locator}}', engines).status, 'configured')
})
