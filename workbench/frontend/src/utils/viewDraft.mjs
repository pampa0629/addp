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

export function buildQueryRequest(descriptor, draft, cursor = '', format = 'json') {
  const predicates = draft.parameters
    .filter(hasParameterValue)
    .map((parameter) => buildPredicate(parameter))

  return {
    select: [...draft.columns],
    filter: predicates.length === 0
      ? null
      : predicates.length === 1
        ? predicates[0]
        : { and: predicates },
    order_by: stableOrder(descriptor),
    page: { limit: draft.pageLimit, cursor },
    format
  }
}

export function buildViewPayload(descriptor, draft, version) {
  const defaults = Object.fromEntries(
    draft.parameters
      .filter(hasParameterValue)
      .map((parameter) => [parameter.key, normalizeParameterValue(parameter)])
  )
  return {
    name: draft.name,
    description: draft.description,
    service_ref: descriptor.ref,
    parameter_definitions: draft.parameters.map((parameter) => ({
      key: parameter.key,
      label: parameter.label,
      control_type: parameter.controlType,
      required: parameter.required
    })),
    query_template: {
      select: [...draft.columns],
      fixed_filter: null,
      parameter_filters: draft.parameters.map((parameter) => ({
        parameter_key: parameter.key,
        field: parameter.field,
        operator: parameter.operator
      })),
      order_by: stableOrder(descriptor),
      page_limit: draft.pageLimit,
      format: 'json'
    },
    default_parameter_values: defaults,
    renderer_type: draft.rendererType,
    renderer_config: rendererConfig(draft),
    ...(version ? { version } : {})
  }
}

function buildPredicate(parameter) {
  const predicate = { field: parameter.field, op: parameter.operator }
  if (!UNARY_OPERATORS.has(parameter.operator)) {
    predicate.value = normalizeParameterValue(parameter)
  }
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
  if (draft.rendererType === 'chart') {
    return { chart_type: draft.chartType, dimension: draft.dimension, measures: [...draft.measures] }
  }
  if (draft.rendererType === 'map') {
    return { geometry_field: draft.geometryField, tooltip_fields: [...draft.tooltipFields] }
  }
  return { columns: [...draft.columns] }
}

function hasScalarValue(value) {
  return value !== '' && value !== null && value !== undefined
}
