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

export function runtimeLayoutStyle(placement) {
  return {
    gridColumn: `${placement.x + 1} / span ${placement.width}`,
    gridRow: `${placement.y + 1} / span ${placement.height}`,
  }
}
