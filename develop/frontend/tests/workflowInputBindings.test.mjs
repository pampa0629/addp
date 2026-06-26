import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'

const source = await readFile(resolve('src/utils/workflowInputBindings.js'), 'utf8')
const mod = await import(`data:text/javascript;charset=utf-8,${encodeURIComponent(source)}`)

assert.deepEqual(
  mod.applyWorkflowInputRefs({
    params: { on: 'region_id' },
    parameters: [
      { name: 'left_df', type: 'dataframe', required: true },
      { name: 'right_df', type: 'dataframe', required: true },
      { name: 'on', type: 'str', required: true }
    ],
    inputEdges: [
      { sourceId: 'load_roads', sourcePort: 'default', sourceType: 'dataframe' },
      { sourceId: 'load_regions', sourcePort: 'default', sourceType: 'dataframe' }
    ]
  }),
  {
    left_df: { $ref: 'load_roads' },
    right_df: { $ref: 'load_regions' },
    on: 'region_id'
  }
)

assert.deepEqual(
  mod.applyWorkflowInputRefs({
    params: {},
    parameters: [
      { name: 'input_gdf', type: 'object', required: true },
      { name: 'mask_gdf', type: 'object', required: true }
    ],
    inputEdges: [
      { sourceId: 'load_roads', sourcePort: 'default', sourceType: 'geodataframe' },
      { sourceId: 'load_boundary', sourcePort: 'default', sourceType: 'geodataframe' }
    ]
  }),
  {
    input_gdf: { $ref: 'load_roads' },
    mask_gdf: { $ref: 'load_boundary' }
  }
)

assert.deepEqual(
  mod.applyWorkflowInputRefs({
    params: { b: 2 },
    parameters: [
      { name: 'a', type: 'float', required: true },
      { name: 'b', type: 'float', required: true }
    ],
    inputEdges: [
      { sourceId: 'sum', sourcePort: 'default', sourceType: 'float' }
    ]
  }),
  {
    a: { $ref: 'sum' },
    b: 2
  }
)

assert.deepEqual(
  mod.applyWorkflowInputRefs({
    params: { source_type: 'table' },
    parameters: null,
    inputEdges: []
  }),
  { source_type: 'table' }
)

assert.throws(
  () => mod.applyWorkflowInputRefs({
    params: {},
    parameters: [{ name: 'query', type: 'str', required: true }],
    inputEdges: [{ sourceId: 'view', sourcePort: 'default', sourceType: 'dataframe' }]
  }),
  /no compatible input parameter/
)

assert.equal(mod.isWorkflowInputParameter({ name: 'left_df', type: 'dataframe' }), true)
assert.equal(mod.isWorkflowInputParameter({ name: 'mask_gdf', type: 'object' }), true)
assert.equal(mod.isWorkflowInputParameter({ name: 'target_geom', type: 'object' }), false)

console.log('workflowInputBindings tests passed')
