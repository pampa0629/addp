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
