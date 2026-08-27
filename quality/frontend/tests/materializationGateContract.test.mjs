import assert from 'node:assert/strict'
import test from 'node:test'
import {
  bindingAlias,
  buildMaterializationGateDocument,
  createMaterializationGateAssertion,
  parseMaterializationGateDocument
} from '../src/utils/materializationGateContract.js'
import { resolveMaterializationGateRouteState } from '../src/utils/materializationGateRouteState.js'

test('materialization gate route canonicalizes dialog and pagination state', () => {
  const state = resolveMaterializationGateRouteState({ create: '1', task_id: '9', page: '2', page_size: '50' })
  assert.equal(state.mode, 'edit')
  assert.deepEqual(state.query, { task_id: '9', page: '2', page_size: '50' })
  assert.equal(state.changed, true)
})

test('materialization gate contract serializes typed conditions without UI metadata', () => {
  const assertion = createMaterializationGateAssertion('predicate_implication', '123e4567-e89b-12d3-a456-426614174000')
  assertion.params.table = 'participation'
  Object.assign(assertion.params.when, { column: 'is_actual', operator: 'eq', value: 'true', value_type: 'boolean' })
  Object.assign(assertion.params.then, { column: 'is_signup', operator: 'is_true' })
  const document = buildMaterializationGateDocument([assertion])
  assert.deepEqual(document.assertions[0].params.when, { column: 'is_actual', operator: 'eq', value: true })
  assert.deepEqual(document.assertions[0].params.then, { column: 'is_signup', operator: 'is_true' })
  assert.equal(parseMaterializationGateDocument(document)[0].params.when.value_type, 'boolean')
})

test('binding aliases are stable SQL-safe names', () => {
  assert.equal(bindingAlias('DWD Outdoor Person', 5), 'dwd_outdoor_person')
  assert.equal(bindingAlias('123', 5), 'table_5')
})
