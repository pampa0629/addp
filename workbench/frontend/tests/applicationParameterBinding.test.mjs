import test from 'node:test'
import assert from 'node:assert/strict'
import { canBindApplicationParameter } from '../src/utils/applicationParameterOptions.mjs'

function fixture() {
  const components = ['a', 'b'].map(id => ({ id, contract_fingerprint: id, parameter_definitions: [{ key: 'p', control_type: 'text' }], query_template: { select: ['id'], named_parameter_bindings: [{ name: 'person', parameter_key: 'p' }] } }))
  const parameters = ['a', 'b'].map(key => ({ key, control_type: 'text', default_value: 'person-a' }))
  return {
    snapshot: { components, parameters, parameter_bindings: components.map(c => ({ component_id: c.id, component_parameter_key: 'p', application_parameter_key: c.id })), selection_bindings: [] },
    descriptors: Object.fromEntries(components.map(c => [c.id, { contract_fingerprint: c.id, input_contract: { named_parameters: [{ name: 'person', type: 'string' }] }, output_contract: { fields: [{ name: 'id', type: 'string' }] } }])),
  }
}
test('sharing a filter requires actual descriptor types and valid defaults, without mutating the draft', () => {
  const { snapshot, descriptors } = fixture()
  const before = structuredClone(snapshot)
  const binding = snapshot.parameter_bindings[1], parameter = snapshot.parameters[0]
  assert.equal(canBindApplicationParameter(snapshot, descriptors, binding, parameter), true)
  assert.deepEqual(snapshot, before)
  descriptors.b.input_contract.named_parameters[0].type = 'uuid'
  assert.equal(canBindApplicationParameter(snapshot, descriptors, binding, parameter), false)
  descriptors.b.input_contract.named_parameters[0].type = 'string'
  descriptors.b.input_contract.named_parameters[0].options = [{ value: 'person-b', labels: { en: 'Person B', 'zh-cn': '乙' } }]
  assert.equal(canBindApplicationParameter(snapshot, descriptors, binding, parameter), false)
  descriptors.b.input_contract.named_parameters[0].options = []
  descriptors.b.contract_fingerprint = 'changed'
  assert.equal(canBindApplicationParameter(snapshot, descriptors, binding, parameter), false)
})
test('sharing cannot strand a selection target or invalidate a scenario value', () => {
  const { snapshot, descriptors } = fixture()
  const binding = snapshot.parameter_bindings[1], parameter = snapshot.parameters[0]
  snapshot.selection_bindings = [{ source_component_id: 'a', assignments: [{ source_field: 'id', application_parameter_key: 'b' }] }]
  assert.equal(canBindApplicationParameter(snapshot, descriptors, binding, parameter), false)
  snapshot.selection_bindings = []
  snapshot.parameter_presets = [{ parameter_values: { a: 'person-b' } }]
  descriptors.b.input_contract.named_parameters[0].options = [{ value: 'person-a', labels: { en: 'Person A', 'zh-cn': '甲' } }]
  assert.equal(canBindApplicationParameter(snapshot, descriptors, binding, parameter), false)
})

test('new component reuse includes all proposed bindings and keeps the source application intact', async () => {
  const { newComponentParameterContext } = await import('../src/utils/applicationParameterOptions.mjs')
  const { snapshot, descriptors } = fixture()
  const before = structuredClone(snapshot)
  const component = { ...structuredClone(snapshot.components[0]), id: 'new', contract_fingerprint: 'new', parameter_definitions: [{ key: 'first', control_type: 'text' }, { key: 'second', control_type: 'text' }], query_template: { named_parameter_bindings: [{ parameter_key: 'first', name: 'first' }, { parameter_key: 'second', name: 'second' }] } }
  const option = value => ({ value, labels: { en: value, 'zh-cn': value } })
  const descriptor = { contract_fingerprint: 'new', input_contract: { named_parameters: [{ name: 'first', type: 'string' }, { name: 'second', type: 'string', options: [option('person-b')] }] } }
  let context = newComponentParameterContext(snapshot, descriptors, component, descriptor)
  let binding = context.snapshot.parameter_bindings.find(b => b.component_id === 'new' && b.component_parameter_key === 'first')
  assert.equal(canBindApplicationParameter(context.snapshot, context.descriptors, binding, snapshot.parameters[0]), true)
  context = newComponentParameterContext(snapshot, descriptors, component, descriptor, { second: 'a' })
  binding = context.snapshot.parameter_bindings.find(b => b.component_id === 'new' && b.component_parameter_key === 'first')
  assert.equal(canBindApplicationParameter(context.snapshot, context.descriptors, binding, snapshot.parameters[0]), false)
  descriptor.input_contract.named_parameters[1].options = [option('person-a')]
  assert.equal(canBindApplicationParameter(context.snapshot, context.descriptors, binding, snapshot.parameters[0]), true)
  assert.deepEqual(snapshot, before)
})
