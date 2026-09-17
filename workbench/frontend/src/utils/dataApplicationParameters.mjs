export function emptyApplicationParameterValue(controlType) {
  if (controlType === 'checkbox') return false
  if (controlType === 'multiselect' || controlType === 'bbox') return []
  if (controlType === 'number' || controlType === 'select') return null
  return ''
}

export function initialApplicationParameterValue(parameter) {
  return Object.prototype.hasOwnProperty.call(parameter, 'default_value')
    ? structuredClone(parameter.default_value)
    : emptyApplicationParameterValue(parameter.control_type)
}

export function defaultApplicationParameterValues(snapshot) {
  return Object.fromEntries((snapshot?.parameters || []).map((parameter) => [
    parameter.key,
    initialApplicationParameterValue(parameter),
  ]))
}

export function applicationParameterPlacement(snapshot) {
  const selectionSources = new Set((snapshot.selection_bindings || []).map(binding => binding.source_component_id))
  const selectionTargets = new Set((snapshot.selection_bindings || []).flatMap(binding => binding.assignments.map(assignment => assignment.application_parameter_key)))
  const consumers = new Map()
  for (const binding of snapshot.parameter_bindings || []) {
    if (!consumers.has(binding.application_parameter_key)) consumers.set(binding.application_parameter_key, new Set())
    consumers.get(binding.application_parameter_key).add(binding.component_id)
  }
  const pageParameters = []
  const componentParameters = {}
  for (const parameter of snapshot.parameters || []) {
    const targets = consumers.get(parameter.key)
    const componentID = targets?.size === 1 ? [...targets][0] : null
    if (componentID && selectionSources.has(componentID) && !selectionTargets.has(parameter.key)) {
      ;(componentParameters[componentID] ||= []).push(parameter)
    } else {
      pageParameters.push(parameter)
    }
  }
  return { pageParameters, componentParameters }
}
