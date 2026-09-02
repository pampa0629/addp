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
