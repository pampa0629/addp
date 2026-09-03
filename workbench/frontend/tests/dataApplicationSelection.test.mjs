import assert from 'node:assert/strict'
import test from 'node:test'
import {
  affectedSelectionComponentIDs,
  compatibleSelectionParameters,
  selectionParameterType,
  selectionSourceFields,
} from '../src/utils/dataApplicationSelection.mjs'

const snapshot = {
  components: [
    { id: 'source', query_template: { select: ['city', 'shape'] } },
    { id: 'target-a', query_template: { parameter_filters: [{ parameter_key: 'city-filter', field: 'city_code', operator: 'eq' }] } },
    { id: 'target-b', query_template: { parameter_filters: [{ parameter_key: 'city-filter', field: 'city_code', operator: 'eq' }] } },
  ],
  parameters: [{ key: 'selected_city', required: true }, { key: 'unused', required: false }],
  parameter_bindings: [
    { application_parameter_key: 'selected_city', component_id: 'target-a', component_parameter_key: 'city-filter' },
    { application_parameter_key: 'selected_city', component_id: 'target-b', component_parameter_key: 'city-filter' },
  ],
}

const descriptors = {
  source: { output_contract: { fields: [{ name: 'city', type: 'string', nullable: false }, { name: 'shape', type: 'geometry' }] } },
  'target-a': { input_contract: { fields: [{ name: 'city_code', type: 'string' }] } },
  'target-b': { input_contract: { fields: [{ name: 'city_code', type: 'string' }] } },
}

test('offers only selected scalar source fields and exact compatible parameters', () => {
  const fields = selectionSourceFields(snapshot, 'source', descriptors.source)
  assert.deepEqual(fields.map((field) => field.name), ['city'])
  assert.equal(selectionParameterType(snapshot, descriptors, 'selected_city'), 'string')
  assert.deepEqual(compatibleSelectionParameters(snapshot, descriptors, fields[0]).map((parameter) => parameter.key), ['selected_city'])
})

test('derives affected components from parameter bindings without storing targets twice', () => {
  assert.deepEqual(affectedSelectionComponentIDs(snapshot, [{ application_parameter_key: 'selected_city' }]), ['target-a', 'target-b'])
})

test('rejects parameter targets with different types or list operators', () => {
  const mismatched = structuredClone(descriptors)
  mismatched['target-b'].input_contract.fields[0].type = 'uuid'
  assert.equal(selectionParameterType(snapshot, mismatched, 'selected_city'), '')
  const listTarget = structuredClone(snapshot)
  listTarget.components[1].query_template.parameter_filters[0].operator = 'in'
  assert.equal(selectionParameterType(listTarget, descriptors, 'selected_city'), '')
})

test('accepts a service named parameter as an exact scalar selection target', () => {
  const namedSnapshot = structuredClone(snapshot)
  namedSnapshot.components[2].query_template = {
    named_parameter_bindings: [{ parameter_key: 'person', name: 'person_id_b' }],
  }
  namedSnapshot.parameter_bindings[1].component_parameter_key = 'person'
  const namedDescriptors = structuredClone(descriptors)
  namedDescriptors['target-b'].input_contract = {
    fields: [],
    named_parameters: [{ name: 'person_id_b', type: 'string', required: true }],
  }
  assert.equal(selectionParameterType(namedSnapshot, namedDescriptors, 'selected_city'), 'string')
})
