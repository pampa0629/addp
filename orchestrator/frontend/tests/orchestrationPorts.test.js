import assert from 'node:assert/strict'
import test from 'node:test'

import {
  arePortTypesCompatible,
  clearParameterBinding,
  executionInputPorts,
  executionOutputPorts,
  parameterBindings,
  setParameterBinding
} from '../src/utils/orchestrationPorts.js'

const contract = {
  input_schema: {
    type: 'object',
    properties: {
      load: {
        type: 'object',
        properties: {
          source: {
            type: 'object',
            title: 'Data source',
            properties: {
              locator: { type: 'string' },
              geometry_column: { type: 'string' }
            }
          }
        }
      },
      buffer: {
        type: 'object',
        properties: {
          distance: { type: 'number', title: 'Distance' }
        }
      }
    }
  },
  input_defaults: {
    load: { source: { locator: 'addp://default', geometry_column: 'geometry' } },
    buffer: { distance: 100 }
  },
  input_ui_schema: {
    load: {
      control: 'group',
      title: 'Load',
      order: 0,
      fields: {
        source: {
          control: 'resource_tree_picker',
          display_name: 'Source',
          order: 0,
          resource_binding: { mode: 'existing' }
        }
      }
    },
    buffer: {
      control: 'group',
      title: 'Buffer',
      order: 1,
      fields: { distance: { order: 0 } }
    }
  },
  output_schema: {
    type: 'object',
    properties: {
      saved: {
        type: 'object',
        title: 'Saved result',
        properties: {
          resource: {
            type: 'object',
            title: 'Resource',
            properties: {
              locator: { type: 'string', format: 'resource-locator' },
              type: { type: 'string' }
            }
          }
        }
      },
      count: { type: 'integer', title: 'Count' }
    }
  }
}

test('Quality table aliases are independent resource inputs with stable upstream bindings', () => {
  const contract = {
    input_schema: { type: 'object', properties: { table_bindings: { type: 'object', properties: { people: { type: 'string', format: 'resource-locator' }, members: { type: 'string', format: 'resource-locator' } } } } },
    input_defaults: { table_bindings: { people: 'addp://engine/2/path/east/people?type=table' } },
    input_ui_schema: { table_bindings: { control: 'group', fields: { people: { control: 'resource_tree_picker' }, members: { control: 'resource_tree_picker' } } } },
  }
  const inputs = executionInputPorts(contract)
  assert.equal(inputs.length, 2)
  const members = inputs.find(input => input.name === 'table_bindings.members')
  assert.equal(members.resource, true)
  assert.deepEqual(members.bindingPath, ['table_bindings', 'members'])
  assert.deepEqual(setParameterBinding({}, members, '{{materialize.outputs.target.locator}}'), { table_bindings: { members: '{{materialize.outputs.target.locator}}' } })
})

test('execution ports use logical resource fields and stable outputs', () => {
  const inputs = executionInputPorts(contract)
  assert.deepEqual(inputs.map(port => [port.name, port.label, port.type]), [
    ['load.source', 'Load / Source', 'string'],
    ['buffer.distance', 'Buffer / Distance', 'number']
  ])
  assert.deepEqual(inputs[0].bindingPath, ['load', 'source', 'locator'])

  const outputs = executionOutputPorts(contract)
  assert.deepEqual(outputs.map(port => [port.name, port.label, port.type]), [
    ['saved.resource.locator', 'Saved result / Resource', 'string'],
    ['count', 'Count', 'integer']
  ])
  assert.equal(arePortTypesCompatible(outputs[0], inputs[0]), true)
  assert.equal(arePortTypesCompatible(outputs[1], inputs[1]), false)
})

test('relation query parameters expose each table as a direct resource input', () => {
  const inputs = executionInputPorts({
    input_schema: {
      type: 'object',
      properties: {
		person: {
          type: 'object',
		  properties: { locator: { type: 'string', format: 'resource-locator' } }
		},
		participation: {
		  type: 'object',
		  properties: { locator: { type: 'string', format: 'resource-locator' } }
        },
        target_locator: { type: 'string' }
      }
    },
    input_defaults: {},
    input_ui_schema: {
	  person: { control: 'resource_tree_picker', resource_binding: { mode: 'existing' }, order: 0 },
	  participation: { control: 'resource_tree_picker', resource_binding: { mode: 'existing' }, order: 1 },
	  target_locator: { order: 2 }
    }
  })

  assert.deepEqual(inputs.map(port => [port.name, port.type, port.bindingPath]), [
	['person', 'string', ['person', 'locator']],
	['participation', 'string', ['participation', 'locator']],
    ['target_locator', 'string', ['target_locator']]
  ])
})

test('parameter bindings preserve resource defaults and revert the whole logical field', () => {
  const input = executionInputPorts(contract)[0]
  const template = '{{produce.outputs.saved.resource.locator}}'
  const parameters = setParameterBinding({}, input, template)
  assert.deepEqual(parameters, {
    load: { source: { locator: template, geometry_column: 'geometry' } }
  })
  assert.deepEqual(parameterBindings(parameters, [input]).map(binding => ({
    stepId: binding.stepId,
    outputPath: binding.outputPath,
    input: binding.inputPort.name
  })), [{
    stepId: 'produce',
    outputPath: ['saved', 'resource', 'locator'],
    input: 'load.source'
  }])
  assert.deepEqual(clearParameterBinding(parameters, input), {})
})

test('string resource picker binds materialization output directly to the query target', () => {
  const original = { target_locator: 'addp://engine/2/path/outdoor/dim_person?type=table', write_mode: 'overwrite' }
  const [input] = executionInputPorts({
    input_schema: { type: 'object', properties: { target_locator: { type: 'string', format: 'resource-locator' } } },
    input_defaults: original,
    input_ui_schema: { target_locator: { control: 'resource_tree_picker', resource_binding: { mode: 'existing' } } }
  })
  const template = '{{materialize.outputs.target_locator}}'
  const bound = setParameterBinding(original, input, template)
  assert.deepEqual(input.bindingPath, ['target_locator'])
  assert.deepEqual(bound, { target_locator: template, write_mode: 'overwrite' })
  assert.equal(parameterBindings(bound, [input])[0].stepId, 'materialize')
  assert.deepEqual(clearParameterBinding(bound, input), { write_mode: 'overwrite' })
  assert.equal(original.target_locator, 'addp://engine/2/path/outdoor/dim_person?type=table')
})

test('object target resource binds its parent and retains target configuration', () => {
  const [input] = executionInputPorts({
    input_schema: { type: 'object', properties: { target: { type: 'object', properties: { parent_locator: { type: 'string' }, name: { type: 'string' } } } } },
    input_defaults: { target: { name: 'default_table' } },
    input_ui_schema: { target: { control: 'resource_tree_picker', resource_binding: { mode: 'target' } } }
  })
  const template = '{{source.outputs.schema_locator}}'
  const bound = setParameterBinding({ target: { name: 'chosen_table' } }, input, template)
  assert.deepEqual(bound, { target: { name: 'chosen_table', parent_locator: template } })
  assert.equal(parameterBindings(bound, [input])[0].stepId, 'source')
})
