const NUMERIC_TYPES = new Set(['int', 'bigint', 'float', 'double', 'decimal'])
const UNARY_OPERATORS = new Set(['is_null', 'is_not_null'])

export function hasParameterValue(parameter) {
  if (UNARY_OPERATORS.has(parameter.operator)) return parameter.value === true
  if (parameter.operator === 'in') return Array.isArray(parameter.value) && parameter.value.length > 0
  if (parameter.operator === 'bbox_intersects') {
    return Array.isArray(parameter.value) && parameter.value.length === 4 && parameter.value.every(hasScalarValue)
  }
  return hasScalarValue(parameter.value)
}

export function createParameterDraft(field, index = 0) {
  const operator = Array.isArray(field?.operators) ? field.operators.find(Boolean) : ''
  if (!field?.name || !operator) return null
  const controlType = controlTypeFor(field, operator)
  return {
    key: `parameter_${index + 1}`,
    label: field.comment || field.name,
    controlType,
    required: false,
    field: field.name,
    operator,
    fieldType: field.type,
    value: emptyControlValue(controlType),
  }
}

export function buildQueryRequest(descriptor, draft, cursor = '', format = 'json') {
  const predicates = draft.parameters.filter(hasParameterValue).map(buildPredicate)
  return {
    select: [...draft.columns],
    filter: predicates.length === 0 ? null : predicates.length === 1 ? predicates[0] : { and: predicates },
    order_by: stableOrder(descriptor),
    page: { limit: draft.pageLimit, cursor },
    format,
  }
}

export function buildComponentConfiguration(descriptor, draft, id) {
  const defaults = Object.fromEntries(
    draft.parameters.filter(hasParameterValue).map((parameter) => [parameter.key, normalizeParameterValue(parameter)]),
  )
  return {
    id,
    title: draft.name.trim(),
    description: draft.description.trim(),
    service_ref: { ...descriptor.ref },
    contract_fingerprint: descriptor.contract_fingerprint,
    parameter_definitions: draft.parameters.map((parameter) => ({
      key: parameter.key,
      label: parameter.label,
      control_type: parameter.controlType,
      required: parameter.required,
    })),
    query_template: {
      select: [...draft.columns],
      fixed_filter: null,
      parameter_filters: draft.parameters.map((parameter) => ({
        parameter_key: parameter.key,
        field: parameter.field,
        operator: parameter.operator,
      })),
      order_by: stableOrder(descriptor),
      page_limit: draft.pageLimit,
      format: 'json',
    },
    default_parameter_values: defaults,
    renderer_type: draft.rendererType,
    renderer_config: rendererConfig(draft),
  }
}

export function draftFromComponent(component, descriptor) {
  const config = component.renderer_config || {}
  return {
    name: component.title,
    description: component.description || '',
    columns: [...(component.query_template?.select || [])],
    pageLimit: component.query_template?.page_limit || descriptor.input_contract.page.default_limit,
    parameters: (component.parameter_definitions || []).map((definition) => {
      const binding = (component.query_template?.parameter_filters || []).find((item) => item.parameter_key === definition.key) || {}
      const field = (descriptor.input_contract.fields || []).find((item) => item.name === binding.field)
      return {
        key: definition.key,
        label: definition.label,
        controlType: definition.control_type,
        required: definition.required,
        field: binding.field,
        operator: binding.operator,
        fieldType: field?.type || 'string',
        value: component.default_parameter_values?.[definition.key] ?? emptyControlValue(definition.control_type),
      }
    }),
    rendererType: component.renderer_type,
    chartType: config.chart_type || 'bar',
    dimension: config.dimension || '',
    measures: [...(config.measures || [])],
    valueItems: (config.items || []).map((item) => ({ ...item })),
    geometryField: config.geometry_field || descriptor.output_contract.spatial?.primary_geometry_field || '',
    mapLabelField: config.label_field || '',
    tooltipFields: [...(config.tooltip_fields || [])],
    mapStyleMode: config.style?.mode || 'uniform',
    mapColorField: config.style?.field || '',
    mapPalette: config.style?.palette || 'primary',
    mapLegendTitle: config.style?.legend_title || '',
  }
}

export function controlTypeFor(field, operator) {
  if (operator === 'bbox_intersects') return 'bbox'
  if (operator === 'in') return 'multiselect'
  if (operator === 'is_null' || operator === 'is_not_null') return 'checkbox'
  if (field?.type === 'bool') return 'select'
  if (field?.type === 'date') return 'date'
  if (field?.type === 'timestamp') return 'datetime'
  if (NUMERIC_TYPES.has(field?.type)) return 'number'
  return 'text'
}

export function emptyControlValue(controlType) {
  if (controlType === 'bbox') return ['', '', '', '']
  if (controlType === 'multiselect') return []
  if (controlType === 'checkbox') return false
  if (controlType === 'number') return null
  return ''
}

function buildPredicate(parameter) {
  const predicate = { field: parameter.field, op: parameter.operator }
  if (!UNARY_OPERATORS.has(parameter.operator)) predicate.value = normalizeParameterValue(parameter)
  return predicate
}

function normalizeParameterValue(parameter) {
  if (parameter.operator === 'bbox_intersects') return parameter.value.map(Number)
  if (parameter.operator === 'in') return parameter.value.map((value) => normalizeScalar(value, parameter.fieldType))
  if (UNARY_OPERATORS.has(parameter.operator)) return true
  return normalizeScalar(parameter.value, parameter.fieldType)
}

function normalizeScalar(value, type) {
  if (NUMERIC_TYPES.has(type)) return Number(value)
  if (type === 'bool') return value === true || value === 'true'
  return value
}

function stableOrder(descriptor) {
  return (descriptor.input_contract?.order?.stable_key || []).map((field) => ({ field, direction: 'asc' }))
}

function rendererConfig(draft) {
  if (draft.rendererType === 'chart') return { chart_type: draft.chartType, dimension: draft.dimension, measures: [...draft.measures] }
  if (draft.rendererType === 'map') return {
    geometry_field: draft.geometryField,
    label_field: draft.mapLabelField,
    tooltip_fields: [...draft.tooltipFields],
    style: {
      mode: draft.mapStyleMode,
      ...(draft.mapStyleMode === 'uniform' ? {} : { field: draft.mapColorField }),
      palette: draft.mapPalette,
      legend_title: draft.mapLegendTitle,
    },
  }
  if (draft.rendererType === 'value') return { items: draft.valueItems.map((item) => ({ ...item })) }
  return { columns: [...draft.columns] }
}

function hasScalarValue(value) {
  return value !== '' && value !== null && value !== undefined
}
