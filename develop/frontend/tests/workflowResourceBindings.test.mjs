import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'

const source = await readFile(resolve('src/utils/workflowResourceBindings.js'), 'utf8')
const mod = await import(`data:text/javascript;charset=utf-8,${encodeURIComponent(source)}`)
const { geometryColumnFactsFromSelection } = await import('../../../common-frontend/basic/src/utils/resourceSelection.js')

const sourcePicker = {
  ui_config: {
    resource_binding: {
      mode: 'existing',
      locator_param: 'source_locator',
      geometry_column_param: 'geometry_field',
      type_param: 'source_kind',
      type_values: { table: 'table', file: 'file' }
    }
  }
}

assert.deepEqual(
  mod.applyResourceBindingSelection(sourcePicker, { geometry_field: 'shape' }, 'addp://engine/1/path/public/roads?type=table', 'table'),
  { source_locator: 'addp://engine/1/path/public/roads?type=table', source_kind: 'table', geometry_field: 'shape' }
)
const formatFilteredPicker = {
  ui_config: { file_formats: ['csv', 'gpkg'] }
}
assert.equal(mod.isResourceFormatSupported(formatFilteredPicker, { type: 'table', label: 'roads' }), true)
assert.equal(mod.isResourceFormatSupported(formatFilteredPicker, { type: 'file', label: 'roads.gpkg' }), true)
assert.equal(mod.isResourceFormatSupported(formatFilteredPicker, { type: 'object', format: 'CSV' }), true)
assert.equal(mod.isResourceFormatSupported(formatFilteredPicker, { type: 'file', label: 'notes.txt' }), false)
const pointCloudPicker = {
  ui_config: {
    data_types: ['point_cloud'],
    target_name_extension: '.copc.laz',
    target_name_kind: 'file'
  }
}
assert.equal(mod.isResourceDataTypeSupported(pointCloudPicker, { data_type: 'point_cloud' }), true)
assert.equal(mod.isResourceDataTypeSupported(pointCloudPicker, { metadata: { data_type: 'model_3d' } }), false)
assert.equal(mod.resourceBindingTargetExtension(pointCloudPicker), '.copc.laz')
assert.equal(mod.resourceBindingTargetNameKind(pointCloudPicker), 'file')
assert.deepEqual(
  geometryColumnFactsFromSelection({
    resource: {
      spatial: {
        primary_geometry_column: 'shape_secondary',
        geometry_columns: ['shape', 'shape_secondary']
      }
    },
    raw: {
      node: {
        metadata: {
          spatial: { geometry: 'ignored_private_shape' }
        }
      }
    }
  }),
  { columns: ['shape', 'shape_secondary'], selected: 'shape_secondary' }
)
assert.equal(mod.resourceBindingInitialLocator(sourcePicker, { source_locator: 'source' }), 'source')
assert.equal(mod.resourceBindingGeometryColumnParam(sourcePicker), 'geometry_field')
assert.deepEqual(mod.missingResourceBindingParams([sourcePicker], {}), ['source_locator'])

const targetPicker = {
  ui_config: {
    resource_binding: {
      mode: 'target',
      parent_locator_param: 'destination_parent',
      name_param: 'destination_name',
      type_param: 'destination_kind',
      type_values: { bucket: 'file' },
      default_params: { write_mode: 'replace' }
    }
  }
}

assert.deepEqual(
  mod.applyResourceBindingSelection(targetPicker, {}, 'addp://engine/2/path/output?type=bucket', 'bucket'),
  {
    destination_parent: 'addp://engine/2/path/output?type=bucket',
    destination_name: '',
    destination_kind: 'file',
    write_mode: 'replace'
  }
)
assert.deepEqual(
  mod.collectResourceBindingParams([sourcePicker], {
    source_locator: 'source',
    source_kind: 'table',
    geometry_field: 'shape'
  }),
  { source_locator: 'source', source_kind: 'table', geometry_field: 'shape' }
)
assert.deepEqual(
  mod.missingResourceBindingParams([targetPicker], { destination_parent: 'parent' }),
  ['destination_name']
)
assert.deepEqual(
  mod.collectResourceBindingParams([targetPicker], {
    destination_parent: 'parent',
    destination_name: 'result.parquet',
    destination_kind: 'file'
  }),
  {
    destination_parent: 'parent',
    destination_name: 'result.parquet',
    destination_kind: 'file'
  }
)

const cleared = mod.clearResourceBindingSelection(targetPicker, {
  destination_parent: 'parent',
  destination_name: 'result',
  destination_kind: 'file',
  write_mode: 'replace'
})
assert.equal(cleared.destination_parent, null)
assert.equal(cleared.destination_name, null)
assert.equal(cleared.destination_kind, null)
assert.equal(cleared.write_mode, 'replace')

console.log('workflowResourceBindings tests passed')
