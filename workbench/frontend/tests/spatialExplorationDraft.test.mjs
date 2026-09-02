import assert from 'node:assert/strict'
import test from 'node:test'
import { buildSpatialExplorationDraft } from '../src/utils/spatialExplorationDraft.mjs'

const fingerprint = (character) => `sha256:${character.repeat(64)}`
const inputField = (name, type, options = {}) => ({
  name, type, nullable: false, selectable: true, filterable: false, operators: [], sortable: false, ...options,
})
const outputField = (name, type, options = {}) => ({ name, type, nullable: false, ...options })

const aggregateDescriptor = {
  ref: { service_type: 'query', service_id: 41 },
  contract_fingerprint: fingerprint('a'),
  input_contract: {
    fields: [
      inputField('region_code', 'string', { filterable: true, operators: ['eq'] }),
      inputField('record_count', 'bigint'),
      inputField('measure_total', 'decimal'),
    ],
    order: { stable_key: ['region_code'] },
    page: { default_limit: 20, max_limit: 500 },
  },
  output_contract: {
    fields: [
      outputField('region_code', 'string', { comment: 'Region' }),
      outputField('record_count', 'bigint', { comment: 'Records' }),
      outputField('measure_total', 'decimal', { comment: 'Total' }),
    ],
  },
}

const spatialDescriptor = {
  ref: { service_type: 'query', service_id: 42 },
  contract_fingerprint: fingerprint('b'),
  input_contract: {
    fields: [
      inputField('region_key', 'string', { filterable: true, operators: ['eq'] }),
      inputField('record_name', 'string'),
      inputField('measure_value', 'double'),
      inputField('shape', 'geometry'),
    ],
    default_selection: ['region_key', 'record_name', 'measure_value', 'shape'],
    order: { stable_key: ['region_key', 'record_name'] },
    page: { default_limit: 25, max_limit: 2000 },
  },
  output_contract: {
    fields: [
      outputField('region_key', 'string'),
      outputField('record_name', 'string', { comment: 'Name' }),
      outputField('measure_value', 'double', { comment: 'Measure' }),
      outputField('shape', 'geometry'),
    ],
    spatial: { primary_geometry_field: 'shape', geometry_fields: [{ name: 'shape' }] },
  },
}

function configuration(overrides = {}) {
  return {
    aggregateDescriptor,
    spatialDescriptor,
    applicationName: 'Regional resource explorer',
    parameterLabel: 'Region',
    defaultValue: 'R-01',
    aggregateFilterField: 'region_code',
    detailFilterField: 'region_key',
    dimensionField: 'region_code',
    chartMeasureField: 'measure_total',
    chartType: 'bar',
    valueItems: [
      { field: 'record_count', label: 'Records', unit: '', precision: 0 },
      { field: 'measure_total', label: 'Total', unit: 'units', precision: 2 },
    ],
    mapLabelField: 'record_name',
    mapTooltipFields: ['record_name', 'measure_value'],
    mapStyleMode: 'continuous',
    mapStyleField: 'measure_value',
    mapPalette: 'success',
    mapLegendTitle: 'Measure',
    tableColumns: ['record_name', 'measure_value'],
    titles: { value: 'Summary', chart: 'Distribution', map: 'Map', table: 'Details' },
    ...overrides,
  }
}

test('compiles a generic four-role spatial exploration snapshot fragment from two descriptors', () => {
  const generated = buildSpatialExplorationDraft(configuration(), (role) => `component-${role}`)

  assert.equal(generated.applicationName, 'Regional resource explorer')
  assert.deepEqual(generated.components.map((component) => component.renderer_type), ['value', 'chart', 'map', 'table'])
  assert.deepEqual(generated.components.map((component) => component.service_ref.service_id), [41, 41, 42, 42])
  assert.deepEqual(generated.parameters, [{ key: 'scope', label: 'Region', control_type: 'text', required: true, default_value: 'R-01' }])
  assert.deepEqual(generated.parameterBindings.map((binding) => binding.component_id), ['component-value', 'component-map', 'component-table'])
  assert.deepEqual(generated.selectionBindings, [{
    source_component_id: 'component-chart',
    assignments: [{ source_field: 'region_code', application_parameter_key: 'scope' }],
  }])
  assert.deepEqual(generated.components[0].renderer_config.items, configuration().valueItems)
  assert.deepEqual(generated.components[1].renderer_config, { chart_type: 'bar', dimension: 'region_code', measures: ['measure_total'] })
  assert.deepEqual(generated.components[2].renderer_config, {
    geometry_field: 'shape', label_field: 'record_name', tooltip_fields: ['record_name', 'measure_value'],
    style: { mode: 'continuous', field: 'measure_value', palette: 'success', legend_title: 'Measure' },
  })
  assert.equal(generated.components[1].query_template.page_limit, 500)
  assert.equal(generated.components[2].query_template.page_limit, 1000)
  assert.equal(generated.components[3].query_template.page_limit, 25)
  assert.deepEqual(generated.placements.map(({ x, y, width, height }) => ({ x, y, width, height })), [
    { x: 0, y: 0, width: 4, height: 6 },
    { x: 4, y: 0, width: 8, height: 6 },
    { x: 0, y: 6, width: 12, height: 10 },
    { x: 0, y: 16, width: 12, height: 8 },
  ])
})

test('requires compatible explicit fields instead of inferring domain semantics', () => {
  const mismatchedDetail = structuredClone(spatialDescriptor)
  Object.assign(mismatchedDetail.input_contract.fields.find((field) => field.name === 'measure_value'), { filterable: true, operators: ['eq'] })
  assert.throws(() => buildSpatialExplorationDraft(configuration({ spatialDescriptor: mismatchedDetail, detailFilterField: 'measure_value' })), /filter_type_mismatch/)
  assert.throws(() => buildSpatialExplorationDraft(configuration({ chartMeasureField: 'region_code' })), /invalid_chart_measure/)
  assert.throws(() => buildSpatialExplorationDraft(configuration({ mapStyleField: 'record_name' })), /invalid_map_style_field/)
  assert.throws(() => buildSpatialExplorationDraft(configuration({ defaultValue: '' })), /missing_default_value/)
  assert.throws(() => buildSpatialExplorationDraft(configuration({ tableColumns: [] })), /missing_table_columns/)
  assert.throws(() => buildSpatialExplorationDraft(configuration({ valueItems: [{ field: 'record_count', label: 'Records', unit: '', precision: null }] })), /invalid_value_item/)
})

test('rejects fields that the service does not expose as selectable', () => {
  const restricted = structuredClone(aggregateDescriptor)
  restricted.input_contract.fields.find((field) => field.name === 'measure_total').selectable = false
  assert.throws(() => buildSpatialExplorationDraft(configuration({ aggregateDescriptor: restricted })), /unselectable_output_field/)
})
