import { buildComponentConfiguration, controlTypeFor, hasParameterValue, synchronizeFieldPresentations } from './componentDraft.mjs'

const NUMERIC_TYPES = new Set(['int', 'bigint', 'float', 'double', 'decimal'])
const THEMATIC_TYPES = new Set(['string', 'bool', 'int', 'bigint', 'float', 'double', 'decimal', 'date', 'time', 'timestamp', 'uuid'])
const MAP_STYLE_MODES = new Set(['uniform', 'categorical', 'continuous'])
const MAP_PALETTES = new Set(['primary', 'success', 'warning', 'danger'])
const CHART_TYPES = new Set(['bar', 'pie'])

export function buildSpatialExplorationDraft(configuration, idFactory = () => crypto.randomUUID()) {
  const aggregate = configuration.aggregateDescriptor
  const detail = configuration.spatialDescriptor
  requireDescriptor(aggregate)
  requireDescriptor(detail)

  const aggregateFilter = requireInputField(aggregate, configuration.aggregateFilterField, 'eq')
  const detailFilter = requireInputField(detail, configuration.detailFilterField, 'eq')
  if (aggregateFilter.type !== detailFilter.type) throw new Error('filter_type_mismatch')

  const dimension = requireSelectableOutputField(aggregate, configuration.dimensionField)
  if (dimension.type !== aggregateFilter.type || dimension.nullable || !THEMATIC_TYPES.has(dimension.type)) throw new Error('invalid_dimension')

  const valueItems = (configuration.valueItems || []).map((item) => {
    const field = requireSelectableOutputField(aggregate, item.field)
    if (!NUMERIC_TYPES.has(field.type)) throw new Error('invalid_value_field')
    const precision = item.precision
    if (!String(item.label || '').trim() || !Number.isInteger(precision) || precision < 0 || precision > 8) throw new Error('invalid_value_item')
    return { field: field.name, label: String(item.label).trim(), unit: String(item.unit || '').trim(), precision }
  })
  if (valueItems.length < 1 || valueItems.length > 4 || new Set(valueItems.map((item) => item.field)).size !== valueItems.length) throw new Error('invalid_value_items')

  const chartMeasure = requireSelectableOutputField(aggregate, configuration.chartMeasureField)
  if (!NUMERIC_TYPES.has(chartMeasure.type)) throw new Error('invalid_chart_measure')
  const chartType = configuration.chartType || 'bar'
  if (!CHART_TYPES.has(chartType)) throw new Error('invalid_chart_type')

  const spatial = detail.output_contract?.spatial
  const geometryField = spatial?.primary_geometry_field
  const geometry = requireSelectableOutputField(detail, geometryField)
  if (geometry.type !== 'geometry') throw new Error('invalid_geometry')

  const mapLabel = configuration.mapLabelField ? requireSelectableOutputField(detail, configuration.mapLabelField) : null
  if (mapLabel && !THEMATIC_TYPES.has(mapLabel.type)) throw new Error('invalid_map_label')
  const mapStyleMode = configuration.mapStyleMode
  if (!MAP_STYLE_MODES.has(mapStyleMode)) throw new Error('invalid_map_style')
  let mapStyleField = null
  if (mapStyleMode !== 'uniform') {
    mapStyleField = requireSelectableOutputField(detail, configuration.mapStyleField)
    if (mapStyleMode === 'continuous' && !NUMERIC_TYPES.has(mapStyleField.type)) throw new Error('invalid_map_style_field')
    if (mapStyleMode === 'categorical' && !THEMATIC_TYPES.has(mapStyleField.type)) throw new Error('invalid_map_style_field')
  }
  if (!MAP_PALETTES.has(configuration.mapPalette)) throw new Error('invalid_map_palette')

  const tooltipFields = uniqueFields(configuration.mapTooltipFields).map((name) => requireSelectableOutputField(detail, name).name)
  const tableColumns = uniqueFields(configuration.tableColumns).map((name) => requireSelectableOutputField(detail, name).name)
  if (tableColumns.length === 0) throw new Error('missing_table_columns')

  const applicationName = String(configuration.applicationName || '').trim()
  const parameterLabel = String(configuration.parameterLabel || '').trim()
  if (!applicationName || !parameterLabel) throw new Error('missing_labels')
  const controlType = controlTypeFor(aggregateFilter, 'eq')
  const parameter = {
    key: 'scope_filter',
    label: parameterLabel,
    controlType,
    required: true,
    field: aggregateFilter.name,
    operator: 'eq',
    fieldType: aggregateFilter.type,
    value: configuration.defaultValue,
  }
  if (!hasParameterValue(parameter)) throw new Error('missing_default_value')

  const ids = {
    value: idFactory('value'),
    chart: idFactory('chart'),
    map: idFactory('map'),
    table: idFactory('table'),
  }
  if (new Set(Object.values(ids)).size !== 4 || Object.values(ids).some((id) => !id)) throw new Error('invalid_component_ids')

  const titles = configuration.titles || {}
  for (const role of Object.keys(ids)) {
    if (!String(titles[role] || '').trim()) throw new Error('missing_component_title')
  }

  const value = buildComponentConfiguration(aggregate, {
    name: titles.value, description: '', columns: uniqueFields(valueItems.map((item) => item.field)), pageLimit: 1,
    rendererType: 'value', valueItems, parameters: [parameter],
  }, ids.value)
  const chart = buildComponentConfiguration(aggregate, withPresentations({
    name: titles.chart, description: '', columns: uniqueFields([dimension.name, chartMeasure.name]), pageLimit: boundedLimit(aggregate, 1000),
    rendererType: 'chart', chartType, dimension: dimension.name, measures: [chartMeasure.name], parameters: [],
  }, aggregate), ids.chart)
  const detailParameter = { ...parameter, field: detailFilter.name, fieldType: detailFilter.type }
  const map = buildComponentConfiguration(detail, withPresentations({
    name: titles.map, description: '',
    columns: uniqueFields([geometry.name, mapLabel?.name, mapStyleField?.name, ...tooltipFields]),
    pageLimit: boundedLimit(detail, 1000), parameters: [detailParameter], rendererType: 'map', geometryField: geometry.name,
    mapLabelField: mapLabel?.name || '', tooltipFields, mapStyleMode, mapColorField: mapStyleField?.name || '',
    mapPalette: configuration.mapPalette, mapLegendTitle: String(configuration.mapLegendTitle || '').trim(),
  }, detail), ids.map)
  const table = buildComponentConfiguration(detail, withPresentations({
    name: titles.table, description: '', columns: tableColumns, pageLimit: boundedLimit(detail, detail.input_contract.page.default_limit),
    rendererType: 'table', parameters: [detailParameter],
  }, detail), ids.table)

  const defaultValue = value.default_parameter_values.scope_filter
  return {
    applicationName,
    pageTitle: applicationName,
    components: [value, chart, map, table],
    parameters: [{ key: 'scope', label: parameterLabel, control_type: controlType, required: true, default_value: defaultValue }],
    parameterBindings: [ids.value, ids.map, ids.table].map((componentID) => ({
      application_parameter_key: 'scope', component_id: componentID, component_parameter_key: 'scope_filter',
    })),
    selectionBindings: [{ source_component_id: ids.chart, assignments: [{ source_field: dimension.name, application_parameter_key: 'scope' }] }],
    placements: [
      { component_id: ids.value, x: 0, y: 0, width: 4, height: 6 },
      { component_id: ids.chart, x: 4, y: 0, width: 8, height: 6 },
      { component_id: ids.map, x: 0, y: 6, width: 12, height: 10 },
      { component_id: ids.table, x: 0, y: 16, width: 12, height: 8 },
    ],
  }
}

function requireDescriptor(descriptor) {
  if (!descriptor?.ref?.service_type || !descriptor?.ref?.service_id || !descriptor.contract_fingerprint || !descriptor.input_contract || !descriptor.output_contract) {
    throw new Error('invalid_descriptor')
  }
}

function requireInputField(descriptor, name, operator) {
  const field = (descriptor.input_contract.fields || []).find((candidate) => candidate.name === name)
  if (!field?.filterable || !Array.isArray(field.operators) || !field.operators.includes(operator)) throw new Error('invalid_filter_field')
  return field
}

function requireOutputField(descriptor, name) {
  const field = (descriptor.output_contract.fields || []).find((candidate) => candidate.name === name)
  if (!field) throw new Error('invalid_output_field')
  return field
}

function requireSelectableOutputField(descriptor, name) {
  const field = requireOutputField(descriptor, name)
  const input = (descriptor.input_contract.fields || []).find((candidate) => candidate.name === name)
  if (!input?.selectable) throw new Error('unselectable_output_field')
  return field
}

function uniqueFields(fields) {
  return [...new Set((fields || []).filter(Boolean))]
}

function boundedLimit(descriptor, preferred) {
  const maximum = Number(descriptor.input_contract.page?.max_limit)
  const requested = Number(preferred)
  if (!Number.isInteger(maximum) || maximum < 1 || !Number.isInteger(requested) || requested < 1) throw new Error('invalid_page_contract')
  return Math.min(maximum, requested)
}

function withPresentations(draft, descriptor) {
  return {
    ...draft,
    fieldPresentations: synchronizeFieldPresentations(draft, descriptor.output_contract?.fields || []),
  }
}
