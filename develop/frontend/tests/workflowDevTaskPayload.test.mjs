import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'

const source = await readFile(resolve('src/utils/workflowDevTaskPayload.js'), 'utf8')
const mod = await import(`data:text/javascript;charset=utf-8,${encodeURIComponent(source)}`)

const validWorkflow = {
  tasks: [
    { id: 'read', operator: 'load', params: { locator: 'addp://engine/12/path/public/city?type=table&item_id=1' }, depends_on: [] },
    { id: 'buffer', operator: 'buffer', params: { distance: 100 }, depends_on: ['read'] }
  ]
}

assert.equal(mod.isStandardWorkflowDefinition(validWorkflow), true)
assert.equal(
  mod.isStandardWorkflowDefinition({
    tasks: [{ id: 'read', operator: 'read_table', params: { table: 'city' } }]
  }),
  false
)
assert.equal(
  mod.isStandardWorkflowDefinition({
    tasks: [{ id: 'read', operator: 'read_table', params: { table: 'city' }, depends_on: ['missing'] }]
  }),
  false
)
assert.equal(
  mod.isStandardWorkflowDefinition({
    tasks: [{ id: 'read', operator: 'load', params: { connection_info: {} }, depends_on: [] }]
  }),
  false
)
assert.equal(
  mod.isStandardWorkflowDefinition({
    tasks: [
      { id: 'a', operator: 'one', params: {}, depends_on: ['b'] },
      { id: 'b', operator: 'two', params: {}, depends_on: ['a'] }
    ]
  }),
  false
)

assert.deepEqual(
  mod.buildWorkflowExecutionConfig({
    workflowEngineId: 12,
    sparkRuntimeId: 34,
    requiresSparkRuntime: true
  }),
  {
    type: 'workflow',
    engine_id: 12,
    engine_specific: { spark_cluster_id: 34 }
  }
)

assert.deepEqual(
  mod.buildWorkflowExecutionConfig({
    workflowEngineId: 12,
    sparkRuntimeId: 34,
    requiresSparkRuntime: false
  }),
  {
    type: 'workflow',
    engine_id: 12
  }
)

assert.deepEqual(
  mod.buildWorkflowDevTaskPayload({
    name: 'city_buffer',
    displayName: 'City Buffer',
    description: 'demo',
    workflow: validWorkflow,
    editorLayout: {
      nodes: { read: { x: 10, y: 20 } },
      viewport: { zoom: 1, translate_x: 0, translate_y: 0 }
    },
    workflowEngineId: 12,
    includeDevType: false
  }),
  {
    name: 'city_buffer',
    display_name: 'City Buffer',
    description: 'demo',
    editor_layout: {
      nodes: { read: { x: 10, y: 20 } },
      viewport: { zoom: 1, translate_x: 0, translate_y: 0 }
    },
    execution_config: {
      type: 'workflow',
      engine_id: 12
    },
    content: {
      workflow_definition: validWorkflow,
      inputs: {}
    }
  }
)

assert.throws(
  () => mod.buildWorkflowDevTaskPayload({
    name: 'invalid_workflow',
    workflow: { tasks: [{ id: 'read', operator: 'read_table', params: {} }] },
    workflowEngineId: 12
  }),
  /workflow_definition/
)

console.log('workflowDevTaskPayload tests passed')
