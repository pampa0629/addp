import { validSelectionValue } from './dataApplicationSelection.mjs'

export const APPLICATION_PRESENTATION_SECTIONS = Object.freeze(['title', 'parameters', 'query_actions'])

function hasValue(value, operator) {
  if (['is_null', 'is_not_null'].includes(operator)) return value === true
  if (value === null || value === undefined || value === '') return false
  if (Array.isArray(value)) return value.length > 0 && value.every((item) => item !== '')
  return true
}

function combineFilters(filters) {
  if (filters.length === 0) return null
  if (filters.length === 1) return filters[0]
  return { and: filters }
}

export function initialApplicationParameterValues(snapshot) {
  return Object.fromEntries((snapshot?.parameters || []).map((parameter) => [
    parameter.key,
    Object.prototype.hasOwnProperty.call(parameter, 'default_value') ? structuredClone(parameter.default_value) : '',
  ]))
}

export function buildComponentQuery(snapshot, component, values, cursor = '') {
  const bindingByTarget = new Map(
    (snapshot?.parameter_bindings || [])
      .filter((binding) => binding.component_id === component.id)
      .map((binding) => [binding.component_parameter_key, binding.application_parameter_key])
  )
  const parameterByKey = new Map((snapshot?.parameters || []).map((parameter) => [parameter.key, parameter]))
  const filters = []
  if (component.query_template.fixed_filter) filters.push(structuredClone(component.query_template.fixed_filter))
  for (const parameterFilter of component.query_template.parameter_filters || []) {
    const applicationKey = bindingByTarget.get(parameterFilter.parameter_key)
    const parameter = parameterByKey.get(applicationKey)
    const value = values?.[applicationKey]
    if (!parameter || !hasValue(value, parameterFilter.operator)) {
      if (parameter?.required) throw new Error(`missing required application parameter: ${applicationKey}`)
      continue
    }
    const filter = { field: parameterFilter.field, op: parameterFilter.operator }
    if (!['is_null', 'is_not_null'].includes(parameterFilter.operator)) filter.value = structuredClone(value)
    filters.push(filter)
  }
  return {
    select: [...component.query_template.select],
    filter: combineFilters(filters),
    order_by: [...(component.query_template.order_by || [])],
    page: { limit: component.query_template.page_limit, cursor },
    format: component.query_template.format,
  }
}

export function buildSelectionUpdate(snapshot, sourceComponentID, descriptor, rows, selection) {
  const binding = (snapshot?.selection_bindings || []).find((item) => item.source_component_id === sourceComponentID)
  if (!binding) return null
  const rowIndex = selection?.row_index
  if (!Number.isInteger(rowIndex) || rowIndex < 0 || rowIndex >= rows.length) {
    throw new Error('invalid result selection')
  }
  const row = rows[rowIndex]
  const parameterByKey = new Map((snapshot?.parameters || []).map((parameter) => [parameter.key, parameter]))
  const outputFieldByName = new Map((descriptor?.output_contract?.fields || []).map((field) => [field.name, field]))
  const parameterValues = {}
  const selectedParameterKeys = new Set()
  for (const assignment of binding.assignments || []) {
    const parameter = parameterByKey.get(assignment.application_parameter_key)
    const field = outputFieldByName.get(assignment.source_field)
    if (!parameter || !field || !Object.prototype.hasOwnProperty.call(row || {}, assignment.source_field)) {
      throw new Error('invalid selection binding')
    }
    const value = row[assignment.source_field]
    if (!validSelectionValue(value, field.type) || (parameter.required && (value === null || value === undefined || value === ''))) {
      throw new Error('invalid selection value')
    }
    parameterValues[parameter.key] = structuredClone(value)
    selectedParameterKeys.add(parameter.key)
  }
  if (selectedParameterKeys.size === 0) throw new Error('empty selection binding')
  const componentIDs = [...new Set(
    (snapshot?.parameter_bindings || [])
      .filter((binding) => selectedParameterKeys.has(binding.application_parameter_key))
      .map((binding) => binding.component_id)
  )]
  if (componentIDs.length === 0) throw new Error('selection binding has no targets')
  return { parameter_values: parameterValues, component_ids: componentIDs }
}

export function runtimeLayoutStyle(placement) {
  return {
    gridColumn: `${placement.x + 1} / span ${placement.width}`,
    gridRow: `${placement.y + 1} / span ${placement.height}`,
  }
}

export function runtimeGridStyle(page) {
  if (page?.display_mode !== 'wallboard') return {}
  const rowCount = Math.max(1, ...(page?.placements || []).map((placement) => placement.y + placement.height))
  return { gridTemplateRows: `repeat(${rowCount}, minmax(0, 1fr))` }
}

export function applicationRefreshDelayMilliseconds(page) {
  const interval = page?.refresh_interval_seconds
  if (page?.display_mode !== 'wallboard' || ![30, 60, 300].includes(interval)) return 0
  return interval * 1000
}

export function canRunApplicationRefresh(page, { hidden = false, querying = false } = {}) {
  return applicationRefreshDelayMilliseconds(page) > 0 && !hidden && !querying
}

export function runtimeSectionVisible(page, section) {
  return page?.visible_sections?.includes(section) === true
}

export function canHideApplicationParameters(snapshot) {
  for (const parameter of snapshot?.parameters || []) {
    if (!parameter.required) continue
    if (!Object.prototype.hasOwnProperty.call(parameter, 'default_value')) return false
    const bindings = (snapshot?.parameter_bindings || []).filter((binding) => binding.application_parameter_key === parameter.key)
    if (bindings.length === 0) return false
    for (const binding of bindings) {
      const component = (snapshot?.components || []).find((item) => item.id === binding.component_id)
      const filter = component?.query_template?.parameter_filters?.find((item) => item.parameter_key === binding.component_parameter_key)
      if (!filter || !hasValue(parameter.default_value, filter.operator)) return false
    }
  }
  return true
}
