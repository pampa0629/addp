import { parameterLabel, parameterOptionsAllow, parameterControlType } from '../../../../common-frontend/basic/src/utils/parameterInput.mjs'
import { defaultFieldPresentation } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'
import { initialApplicationParameterValue } from './dataApplicationParameters.mjs'

const NUMERIC_TYPES = new Set(['int', 'bigint', 'float', 'double', 'decimal'])
const UNARY_OPERATORS = new Set(['is_null', 'is_not_null'])

export function configureSelectionListDraft(draft, descriptor, { searchField, labelField, valueField, searchLabel }) {
  draft.rendererType = 'table'
  draft.columns = [...new Set([labelField, valueField].filter(Boolean))]
  draft.parameters = draft.parameters.filter(parameter => parameter.bindingKind === 'named')
  const field = descriptor.input_contract.fields.find(field => field.name === searchField && field.type === 'string' && field.filterable && field.operators?.includes('contains'))
  if (field) {
    const parameter = createParameterDraft(field, draft.parameters.length)
    const keys = new Set(draft.parameters.map(parameter => parameter.key))
    let index = draft.parameters.length + 1
    while (keys.has(parameter.key)) parameter.key = `parameter_${++index}`
    parameter.operator = 'contains'
    parameter.label = searchLabel
    draft.parameters.push(parameter)
  }
  draft.fieldPresentations = synchronizeFieldPresentations(draft, descriptor.output_contract.fields)
}

// Suggestions only select fields; the existing component compiler owns the output.
export function componentDisplaySuggestions(descriptor) {
  if (!descriptor) return []
  const selectable = new Set((descriptor.input_contract.fields || []).filter(field => field.selectable).map(field => field.name))
  const outputs = (descriptor.output_contract.fields || []).filter(field => selectable.has(field.name))
  const defaults = descriptor.input_contract.default_selection || []
  const fields = [...defaults.map(name => outputs.find(field => field.name === name)).filter(Boolean), ...outputs.filter(field => !defaults.includes(field.name))]
  if (!fields.length) return []
  const columns = defaults.filter(name => outputs.some(field => field.name === name))
  const suggestions = [{ key: 'table', rendererType: 'table', columns: columns.length ? columns : fields.map(field => field.name) }]
  const measure = fields.find(field => NUMERIC_TYPES.has(field.type))
  const dimension = fields.find(field => ['string', 'uuid', 'bool'].includes(field.type))
  const temporal = fields.find(field => ['date', 'time', 'timestamp'].includes(field.type) && descriptor.input_contract.order?.stable_key?.includes(field.name))
  for (const [key, field] of [['bar', dimension], ['line', temporal]]) {
    if (measure && field) suggestions.push({ key, rendererType: 'chart', chartType: key, dimension: field.name, measures: [measure.name], columns: [field.name, measure.name] })
  }
  const geometry = descriptor.output_contract.spatial?.primary_geometry_field
  if (geometry && fields.some(field => field.name === geometry && field.type === 'geometry')) {
    suggestions.push({ key: 'map', rendererType: 'map', geometryField: geometry, columns: [geometry] })
  }
  return suggestions
}

export function hasParameterValue(parameter) {
  if (UNARY_OPERATORS.has(parameter.operator)) return parameter.value === true
  if (parameter.operator === 'in') return Array.isArray(parameter.value) && parameter.value.length > 0
  if (parameter.operator === 'bbox_intersects') {
    return Array.isArray(parameter.value) && parameter.value.length === 4 && parameter.value.every(hasScalarValue)
  }
  return hasScalarValue(parameter.value)
}

export function requiredParameterValuesPresent(parameters) {
  return parameters.every((parameter) => (!parameter.required || hasParameterValue(parameter)) && (!hasParameterValue(parameter) || parameterOptionsAllow(parameter.options, parameter.value)))
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
    bindingKind: 'filter',
    field: field.name,
    operator,
    fieldType: field.type,
    value: emptyControlValue(controlType),
  }
}

export function createNamedParameterDraft(parameter, index = 0, locale = 'zh-cn') {
  if (!parameter?.name || !parameter?.type) return null
  const controlType = controlTypeFor({ type: parameter.type }, 'eq')
  return {
    key: parameter.name || `parameter_${index + 1}`,
    label: parameter.presentation ? parameterLabel(parameter, locale) : parameter.description || parameter.name,
    presentation: parameter.presentation,
    description: parameter.description,
    controlType,
    required: parameter.required === true,
    bindingKind: 'named',
    options: parameter.options || [],
    name: parameter.name,
    fieldType: parameter.type,
    operator: 'eq',
    value: Object.prototype.hasOwnProperty.call(parameter, 'default') ? structuredClone(parameter.default) : emptyControlValue(controlType),
  }
}

export function buildQueryRequest(descriptor, draft, cursor = '', format = 'json') {
  if (draft.parameters.some((parameter) => hasParameterValue(parameter) && !parameterOptionsAllow(parameter.options, parameter.value))) throw new Error('parameter-options: invalid-value')
  const predicates = draft.parameters.filter((parameter) => parameter.bindingKind !== 'named' && hasParameterValue(parameter)).map(buildPredicate)
  if (draft.fixedFilter) predicates.unshift(JSON.parse(JSON.stringify(draft.fixedFilter)))
  const parameters = Object.fromEntries(
    draft.parameters
      .filter((parameter) => parameter.bindingKind === 'named' && hasParameterValue(parameter))
      .map((parameter) => [parameter.name, normalizeParameterValue(parameter)]),
  )
  return {
    parameters,
    select: [...draft.columns],
    filter: predicates.length === 0 ? null : predicates.length === 1 ? predicates[0] : { and: predicates },
    order_by: draft.orderBy?.map(item => ({ ...item })) || stableOrder(descriptor),
    page: { limit: draft.pageLimit, cursor },
    format,
  }
}

export function applyApplicationParameterDefaults(draft, snapshot, componentID) {
  const bindings = new Map((snapshot.parameter_bindings || []).filter(binding => binding.component_id === componentID).map(binding => [binding.component_parameter_key, binding.application_parameter_key]))
  const parameters = new Map((snapshot.parameters || []).map(parameter => [parameter.key, parameter]))
  for (const parameter of draft.parameters) {
    if (!bindings.has(parameter.key)) continue
    const source = parameters.get(bindings.get(parameter.key))
    parameter.value = source ? initialApplicationParameterValue(JSON.parse(JSON.stringify(source))) : emptyControlValue(parameter.controlType)
  }
}

export function buildComponentConfiguration(descriptor, draft, id, originalComponent = null) {
  const sameService = originalComponent?.service_ref?.service_type === descriptor.ref.service_type
    && originalComponent?.service_ref?.service_id === descriptor.ref.service_id
    && originalComponent?.contract_fingerprint === descriptor.contract_fingerprint
  const parameterDefaults = draft.parameters.map(parameter => {
    if (!sameService) return parameter
    const binding = parameter.bindingKind === 'named'
      ? originalComponent.query_template.named_parameter_bindings?.find(binding => binding.parameter_key === parameter.key && binding.name === parameter.name)
      : originalComponent.query_template.parameter_filters?.find(binding => binding.parameter_key === parameter.key && binding.field === parameter.field && binding.operator === parameter.operator)
    if (!binding) return parameter
    return { ...parameter, value: originalComponent.default_parameter_values?.[parameter.key] ?? emptyControlValue(parameter.controlType) }
  })
  const defaults = Object.fromEntries(
    parameterDefaults.filter(hasParameterValue).map((parameter) => [parameter.key, normalizeParameterValue(parameter)]),
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
      fixed_filter: draft.fixedFilter ? JSON.parse(JSON.stringify(draft.fixedFilter)) : null,
      parameter_filters: draft.parameters.filter((parameter) => parameter.bindingKind !== 'named').map((parameter) => ({
        parameter_key: parameter.key,
        field: parameter.field,
        operator: parameter.operator,
      })),
      named_parameter_bindings: draft.parameters.filter((parameter) => parameter.bindingKind === 'named').map((parameter) => ({
        parameter_key: parameter.key,
        name: parameter.name,
      })),
      order_by: draft.orderBy?.map(item => ({ ...item })) || stableOrder(descriptor),
      page_limit: draft.pageLimit,
      format: 'json',
    },
    default_parameter_values: defaults,
    renderer_type: draft.rendererType,
    renderer_config: buildRendererConfig(draft),
  }
}

export function draftFromComponent(component, descriptor) {
  const config = component.renderer_config || {}
  const draft = {
    name: component.title,
    description: component.description || '',
    columns: [...(component.query_template?.select || [])],
    fixedFilter: component.query_template.fixed_filter || null,
    orderBy: component.query_template.order_by?.map(item => ({ ...item })) || null,
    pageLimit: component.query_template?.page_limit || descriptor.input_contract.page.default_limit,
    parameters: (component.parameter_definitions || []).map((definition) => {
      const namedBinding = (component.query_template?.named_parameter_bindings || []).find((item) => item.parameter_key === definition.key)
      const filterBinding = (component.query_template?.parameter_filters || []).find((item) => item.parameter_key === definition.key)
      const binding = namedBinding || filterBinding || {}
      const field = namedBinding
        ? (descriptor.input_contract.named_parameters || []).find((item) => item.name === namedBinding.name)
        : (descriptor.input_contract.fields || []).find((item) => item.name === filterBinding?.field)
      return {
        key: definition.key,
        label: definition.label,
        controlType: definition.control_type,
        required: definition.required,
        bindingKind: namedBinding ? 'named' : 'filter',
        name: namedBinding?.name,
        field: filterBinding?.field,
        operator: namedBinding ? 'eq' : filterBinding?.operator,
        fieldType: field?.type || 'string',
        options: namedBinding ? field?.options || [] : [],
        presentation: field?.presentation,
        description: field?.description,
        value: component.default_parameter_values?.[definition.key] ?? emptyControlValue(definition.control_type),
      }
    }),
    rendererType: component.renderer_type,
    chartType: config.chart_type || 'bar',
    totalAsValue: config.total_as_value === true,
    resultNameField: config.result_name_field || '',
    dimension: config.dimension || '',
    measures: [...(config.measures || [])],
    valueItems: (config.items || []).map(valueItemDraft),
    fieldPresentations: (config.field_presentations || []).map((item) => presentationDraft(item, descriptor.output_contract?.fields || [])),
    geometryField: config.geometry_field || descriptor.output_contract.spatial?.primary_geometry_field || '',
    mapLabelField: config.label_field || '',
    tooltipFields: [...(config.tooltip_fields || [])],
    mapStyleMode: config.style?.mode || 'uniform',
    mapColorField: config.style?.field || '',
    mapPalette: config.style?.palette || 'primary',
    mapLegendTitle: config.style?.legend_title || '',
  }
  draft.fieldPresentations = synchronizeFieldPresentations(draft, descriptor.output_contract?.fields || [])
  return draft
}

export function synchronizeFieldPresentations(draft, fields = []) {
  if (draft?.rendererType === 'value') return []
  const existing = new Map((draft?.fieldPresentations || []).map((item) => [item.field, item]))
  const fieldFacts = new Map((fields || []).map((field) => [field.name, field]))
  return rendererFieldNames(draft).map((name) => {
    const field = fieldFacts.get(name)
    if (!field) return null
    const defaults = defaultFieldPresentation(field)
    const current = existing.get(name)
    return {
      ...defaults,
      period: { grain_parameter: '', start_parameter: '', end_parameter: '' },
      ...(current || {}),
      field: name,
      fieldType: field.type,
      ...(draft.rendererType === 'table' ? {} : { width: null }),
    }
  }).filter(Boolean)
}

export function buildRendererConfig(draft) {
  const presentations = serializeFieldPresentations(draft)
  const withPresentations = (config) => presentations.length > 0
    ? { ...config, field_presentations: presentations }
    : config
  if (draft.rendererType === 'chart') return withPresentations({ chart_type: draft.chartType, ...(draft.resultNameField ? { result_name_field: draft.resultNameField } : {}), ...(draft.totalAsValue ? { total_as_value: true } : {}), dimension: draft.dimension, measures: [...draft.measures] })
  if (draft.rendererType === 'map') return withPresentations({
    geometry_field: draft.geometryField,
    label_field: draft.mapLabelField,
    tooltip_fields: [...draft.tooltipFields],
    style: {
      mode: draft.mapStyleMode,
      ...(draft.mapStyleMode === 'uniform' ? {} : { field: draft.mapColorField }),
      palette: draft.mapPalette,
      legend_title: draft.mapLegendTitle,
    },
  })
  if (draft.rendererType === 'value') return { items: draft.valueItems.map(serializeValueItem) }
  return withPresentations({ columns: [...draft.columns] })
}

export function controlTypeFor(field, operator) {
  if (operator === 'bbox_intersects') return 'bbox'
  if (operator === 'in') return 'multiselect'
  if (operator === 'is_null' || operator === 'is_not_null') return 'checkbox'
  return parameterControlType(field?.type)
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

function rendererFieldNames(draft) {
  const source = draft.rendererType === 'chart'
    ? [draft.dimension, ...(draft.measures || []), draft.resultNameField]
    : draft.rendererType === 'map'
      ? [draft.mapLabelField, ...(draft.tooltipFields || []), draft.mapStyleMode === 'uniform' ? '' : draft.mapColorField]
      : draft.rendererType === 'table'
        ? draft.columns || []
        : []
  return [...new Set(source.filter(Boolean))]
}

function serializeFieldPresentations(draft) {
  const used = new Set(rendererFieldNames(draft))
  return (draft.fieldPresentations || []).filter((item) => used.has(item.field)).map((item) => {
    const result = { field: item.field, label: String(item.label || '').trim() }
    if (NUMERIC_TYPES.has(item.fieldType)) {
      if (String(item.unit || '').trim()) result.unit = String(item.unit).trim()
      if (Number.isInteger(item.precision)) result.precision = item.precision
    }
    if (['date', 'time', 'timestamp'].includes(item.fieldType) && item.temporalFormat) result.temporal_format = item.temporalFormat
    if (item.temporalFormat === 'period') result.period = { ...item.period }
    if (draft.rendererType === 'table' && Number.isInteger(item.width)) result.width = item.width
    const stateRules = serializeStateRules(item.stateRules)
    if (stateRules.length > 0) result.state_rules = stateRules
    const valueLabels = (item.valueLabels || []).map(({ value, label }) => ({ value, label: String(label || '').trim() }))
    if (valueLabels.length > 0) result.value_labels = valueLabels
    return result
  })
}

function serializeStateRules(rules = []) {
  return rules.map((rule) => ({
    operator: rule.operator,
    operand: rule.operand,
    label: String(rule.label || '').trim(),
    tone: rule.tone,
  }))
}

function serializeValueItem(item) {
  const stateRules = serializeStateRules(item.stateRules)
  return {
    field: item.field,
    label: String(item.label || '').trim(),
    unit: String(item.unit || '').trim(),
    precision: item.precision,
    ...(stateRules.length > 0 ? { state_rules: stateRules } : {}),
  }
}

function presentationDraft(item, fields) {
  const field = (fields || []).find((candidate) => candidate.name === item.field) || { name: item.field, type: '' }
  const defaults = defaultFieldPresentation(field)
  return {
    ...defaults,
    field: item.field,
    label: item.label,
    unit: item.unit || '',
    precision: Object.prototype.hasOwnProperty.call(item, 'precision') ? item.precision : defaults.precision,
    temporalFormat: item.temporal_format || '',
    period: { grain_parameter: '', start_parameter: '', end_parameter: '', ...item.period },
    width: Object.prototype.hasOwnProperty.call(item, 'width') ? item.width : null,
    valueLabels: (item.value_labels || []).map((entry) => ({ ...entry })),
    stateRules: (item.state_rules || []).map((rule) => ({ ...rule })),
  }
}

function valueItemDraft(item) {
  return {
    field: item.field,
    label: item.label,
    unit: item.unit || '',
    precision: item.precision,
    stateRules: (item.state_rules || []).map((rule) => ({ ...rule })),
  }
}

function hasScalarValue(value) {
  return value !== '' && value !== null && value !== undefined
}
