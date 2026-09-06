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

function componentParameterOperator(snapshot, binding) {
  const component = (snapshot?.components || []).find((item) => item.id === binding.component_id)
  const filter = component?.query_template?.parameter_filters?.find((item) => item.parameter_key === binding.component_parameter_key)
  if (filter) return filter.operator
  const named = component?.query_template?.named_parameter_bindings?.find((item) => item.parameter_key === binding.component_parameter_key)
  return named ? 'eq' : ''
}

function requiredApplicationParametersExecutable(snapshot, values, requireDeclaredValues = false) {
  for (const parameter of snapshot?.parameters || []) {
    if (!parameter.required) continue
    if (requireDeclaredValues && !Object.prototype.hasOwnProperty.call(parameter, 'default_value')) return false
    const bindings = (snapshot?.parameter_bindings || []).filter((binding) => binding.application_parameter_key === parameter.key)
    if (bindings.length === 0) return false
    for (const binding of bindings) {
      const operator = componentParameterOperator(snapshot, binding)
      if (!operator || !hasValue(values?.[parameter.key], operator)) return false
    }
  }
  return true
}

export function initialApplicationParameterValues(snapshot) {
  return Object.fromEntries((snapshot?.parameters || []).map((parameter) => [
    parameter.key,
    Object.prototype.hasOwnProperty.call(parameter, 'default_value') ? structuredClone(parameter.default_value) : '',
  ]))
}

export function componentIDsForApplicationParameters(snapshot, parameterKeys) {
  const changedKeys = new Set(parameterKeys || [])
  return [...new Set(
    (snapshot?.parameter_bindings || [])
      .filter((binding) => changedKeys.has(binding.application_parameter_key))
      .map((binding) => binding.component_id)
      .filter(Boolean)
  )]
}

export function invalidateApplicationParameterResults(snapshot, componentStates, parameterKeys) {
  const componentIDs = componentIDsForApplicationParameters(snapshot, parameterKeys)
  for (const componentID of componentIDs) {
    const current = componentStates?.[componentID]
    if (!current) continue
    current.requests.invalidate()
    current.querying = false
    current.exporting = false
    current.query_error = ''
    current.query_completed = false
    current.rows = []
    current.page = { has_more: false, next_cursor: '' }
    current.cursors = ['']
    current.cursor_index = 0
  }
  return componentIDs
}

export function commitLatestComponentDescriptorState(current, request, componentID, nextState) {
  if (!current.descriptorRequests.isCurrent(request, componentID)) return false
  Object.assign(current, nextState)
  return true
}

export function commitLatestDataApplicationLoad(requests, request, currentApplicationID, commit) {
  const targetID = String(currentApplicationID || '').trim()
  if (!requests.isCurrent(request, targetID)) return false
  commit()
  return true
}

export function buildComponentQuery(snapshot, component, values, cursor = '', format = component.query_template.format) {
  const bindingByTarget = new Map(
    (snapshot?.parameter_bindings || [])
      .filter((binding) => binding.component_id === component.id)
      .map((binding) => [binding.component_parameter_key, binding.application_parameter_key])
  )
  const parameterByKey = new Map((snapshot?.parameters || []).map((parameter) => [parameter.key, parameter]))
  const filters = []
	const parameters = {}
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
	for (const namedBinding of component.query_template.named_parameter_bindings || []) {
	  const applicationKey = bindingByTarget.get(namedBinding.parameter_key)
	  const parameter = parameterByKey.get(applicationKey)
	  const value = values?.[applicationKey]
	  if (!parameter || !hasValue(value, 'eq')) {
	    if (parameter?.required) throw new Error(`missing required application parameter: ${applicationKey}`)
	    continue
	  }
	  parameters[namedBinding.name] = structuredClone(value)
	}
  return {
	parameters,
    select: [...component.query_template.select],
    filter: combineFilters(filters),
    order_by: [...(component.query_template.order_by || [])],
    page: { limit: component.query_template.page_limit, cursor },
    format,
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
  const componentIDs = componentIDsForApplicationParameters(snapshot, selectedParameterKeys)
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

export function componentBlockingError(state) {
  return state?.contract_error || state?.descriptor_error || ''
}

export function canExecuteComponentQuery(state) {
  return Boolean(state?.descriptor) && !state?.contract_error
}

export function canAttemptApplicationQuery(components, states) {
  return (components || []).length > 0 && components.every((component) => !states?.[component.id]?.contract_error)
}

export function canRunPublishedApplicationInitialQuery(snapshot, states) {
  const components = snapshot?.components || []
  if (components.length === 0 || !components.every((component) => canExecuteComponentQuery(states?.[component.id]))) return false
  const values = initialApplicationParameterValues(snapshot)
  if (!requiredApplicationParametersExecutable(snapshot, values, true)) return false
  try {
    components.forEach((component) => buildComponentQuery(snapshot, component, values))
    return true
  } catch {
    return false
  }
}

export function canRunApplicationRefresh(page, { hidden = false, querying = false } = {}) {
  return applicationRefreshDelayMilliseconds(page) > 0 && !hidden && !querying
}

export function runtimeSectionVisible(page, section) {
  return page?.visible_sections?.includes(section) === true
}

export function canHideApplicationParameters(snapshot) {
  return requiredApplicationParametersExecutable(snapshot, initialApplicationParameterValues(snapshot), true)
}
