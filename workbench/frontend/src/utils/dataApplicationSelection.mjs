const STRING_TYPES = new Set(['string', 'date', 'time', 'timestamp', 'uuid'])
const INTEGER_TYPES = new Set(['int', 'bigint'])
const NUMBER_TYPES = new Set(['float', 'double', 'decimal'])
const SCALAR_OPERATORS = new Set(['eq', 'ne', 'lt', 'lte', 'gt', 'gte'])

export function isSelectionScalarType(fieldType) {
  return STRING_TYPES.has(fieldType) || INTEGER_TYPES.has(fieldType) || NUMBER_TYPES.has(fieldType) || fieldType === 'bool'
}

export function isSelectionScalarOperator(operator) {
  return SCALAR_OPERATORS.has(operator)
}

export function validSelectionValue(value, fieldType) {
  if (value === null || value === undefined) return true
  if (STRING_TYPES.has(fieldType)) return typeof value === 'string'
  if (fieldType === 'bool') return typeof value === 'boolean'
  if (INTEGER_TYPES.has(fieldType)) return typeof value === 'number' && Number.isInteger(value)
  if (NUMBER_TYPES.has(fieldType)) return typeof value === 'number' && Number.isFinite(value)
  return false
}

export function selectionSourceFields(snapshot, sourceComponentID, descriptor) {
  const component = (snapshot?.components || []).find((item) => item.id === sourceComponentID)
  const selected = new Set(component?.query_template?.select || [])
  return (descriptor?.output_contract?.fields || []).filter((field) => selected.has(field.name) && isSelectionScalarType(field.type))
}

export function selectionParameterType(snapshot, descriptors, applicationParameterKey) {
  const types = new Set()
  const targets = (snapshot?.parameter_bindings || []).filter((binding) => binding.application_parameter_key === applicationParameterKey)
  if (targets.length === 0) return ''
  for (const binding of targets) {
    const component = (snapshot?.components || []).find((item) => item.id === binding.component_id)
    const parameterFilter = (component?.query_template?.parameter_filters || []).find((filter) => filter.parameter_key === binding.component_parameter_key)
	  if (parameterFilter) {
		const field = (descriptors?.[binding.component_id]?.input_contract?.fields || []).find((item) => item.name === parameterFilter.field)
		if (!field || !isSelectionScalarOperator(parameterFilter.operator)) return ''
		types.add(field.type)
		continue
	  }
	  const namedBinding = (component?.query_template?.named_parameter_bindings || []).find((item) => item.parameter_key === binding.component_parameter_key)
	  const namedParameter = (descriptors?.[binding.component_id]?.input_contract?.named_parameters || []).find((item) => item.name === namedBinding?.name)
	  if (!namedParameter || !isSelectionScalarType(namedParameter.type)) return ''
	  types.add(namedParameter.type)
  }
  return types.size === 1 ? [...types][0] : ''
}

export function compatibleSelectionParameters(snapshot, descriptors, sourceField) {
  if (!sourceField) return []
  return (snapshot?.parameters || []).filter((parameter) =>
    selectionParameterType(snapshot, descriptors, parameter.key) === sourceField.type &&
    (!parameter.required || !sourceField.nullable)
  )
}

export function affectedSelectionComponentIDs(snapshot, assignments) {
  const parameterKeys = new Set((assignments || []).map((assignment) => assignment.application_parameter_key))
  return [...new Set(
    (snapshot?.parameter_bindings || [])
      .filter((binding) => parameterKeys.has(binding.application_parameter_key))
      .map((binding) => binding.component_id)
  )]
}
