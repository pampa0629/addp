import assert from 'node:assert/strict'

const mod = await import('../src/utils/workflowResourceBindings.js')
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
assert.equal(mod.isResourceFormatSupported(formatFilteredPicker, { type: 'object', metadata: { format: 'CSV' } }), true)
assert.equal(mod.isResourceFormatSupported(formatFilteredPicker, { type: 'file', label: 'notes.txt' }), false)
const tiffPicker = { ui_config: { file_formats: ['tiff'] } }
const tiffResource = {
  id: 'addp://engine/2/path/rasters/mosaic-c.tif?type=object&item_id=21',
  locator: 'addp://engine/2/path/rasters/mosaic-c.tif?type=object&item_id=21',
  label: 'mosaic-c.tif', type: 'object', hasChildren: false, children: [],
  metadata: { format: 'tiff', data_type: 'raster' }
}
assert.equal(mod.isResourceFormatSupported(tiffPicker, tiffResource), true)
assert.equal(mod.isResourceFormatSupported(tiffPicker, { ...tiffResource, type: 'file' }), true)
assert.equal(mod.isResourceFormatSupported(tiffPicker, {
  ...tiffResource, label: 'unsupported.tiff', metadata: { format: 'jpeg' }
}), false)
assert.equal(mod.isResourceFormatSupported(tiffPicker, {
  ...tiffResource, label: 'without-facts.tiff', metadata: {}
}), true)
assert.equal(mod.isResourceFormatSupported(tiffPicker, {
  ...tiffResource, metadata: {}, format: 'tiff', attributes: { format: 'tiff' }
}), false)
const pointCloudPicker = {
  ui_config: {
    data_types: ['point_cloud'],
    target_name_extension: '.copc.laz',
    target_name_kind: 'file'
  }
}
assert.equal(mod.isResourceDataTypeSupported(pointCloudPicker, { data_type: 'point_cloud' }), true)
assert.equal(mod.isResourceDataTypeSupported(pointCloudPicker, { metadata: { data_type: 'model_3d' } }), false)
for (const type of ['engine', 'schema', 'database', 'bucket', 'directory', 'dir', 'prefix', 'root', 'service', 'server']) {
  assert.equal(mod.isResourceDataTypeSupported(pointCloudPicker, { type }), true)
}
assert.equal(mod.isResourceDataTypeSupported(pointCloudPicker, { type: 'file', metadata: {} }), false)
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

const savePicker = { ui_config: { engine_types: ['postgresql', 'mysql', 'doris', 'nfs', 'minio'] } }
for (const engine_type of savePicker.ui_config.engine_types) {
  assert.equal(mod.isResourceEngineSupported(savePicker, { engine_type }), true)
}
for (const engine_type of ['hdfs', 'elasticsearch', 'mongodb', 'oracle', undefined]) {
  assert.equal(mod.isResourceEngineSupported(savePicker, { engine_type }), false)
}
assert.equal(mod.isResourceEngineSupported({ ui_config: { engine_types: [] } }, { engine_type: 'nfs' }), false)
assert.equal(mod.isResourceEngineSupported({}, { engine_type: 'hdfs' }), true)
const nfsPicker = { ui_config: { engine_types: ['nfs'] } }
assert.equal(mod.isResourceEngineSupported(nfsPicker, { engine_type: 'nfs' }), true)
assert.equal(mod.isResourceEngineSupported(nfsPicker, { engine_type: 'minio' }), false)

console.log('workflowResourceBindings tests passed')

const writePicker = { ui_config: { engine_types: ['postgresql'], storage_operation: 'write' } }
assert.equal(mod.isResourceEngineSupported(writePicker, { engine_type: 'postgresql', capabilities: { storage: { store: { batch_read: true } } } }), false)
assert.equal(mod.isResourceEngineSupported(writePicker, { engine_type: 'postgresql', capabilities: { storage: { store: { table_write_session: true } } } }), true)
assert.equal(mod.isResourceEngineSupported(writePicker, { engine_type: 'postgresql' }), false)
assert.equal(mod.isResourceEngineSupported({ ui_config: { engine_types: [], storage_operation: 'write' } }, { engine_type: 'postgresql', capabilities: { storage: { store: { batch_write: true } } } }), false)
