export function getResourceBinding(parameter) {
  return parameter?.ui_config?.resource_binding || null
}

export function isTargetResourceBinding(parameter) {
  return getResourceBinding(parameter)?.mode === 'target'
}

export function isResourceFormatSupported(parameter, node) {
  const nodeType = String(node?.type || '').toLowerCase()
  if (!['file', 'object'].includes(nodeType)) return true
  const formats = (parameter?.ui_config?.file_formats || []).map(value => String(value).toLowerCase())
  if (formats.length === 0) return true
  const explicitFormat = String(node?.format || node?.attributes?.format || '').toLowerCase().replace(/^\./, '')
  if (explicitFormat) return formats.includes(explicitFormat)
  const label = String(node?.label || node?.name || '')
  const extension = label.includes('.') ? label.split('.').pop().toLowerCase() : ''
  return extension ? formats.includes(extension) : false
}

export function isResourceDataTypeSupported(parameter, node) {
  const dataTypes = (parameter?.ui_config?.data_types || []).map(value => String(value).toLowerCase())
  if (dataTypes.length === 0) return true
  const dataType = String(node?.data_type || node?.metadata?.data_type || node?.attributes?.data_type || '').toLowerCase()
  return dataType ? dataTypes.includes(dataType) : false
}

export function resourceBindingTargetExtension(parameter) {
  return String(parameter?.ui_config?.target_name_extension || '')
}

export function resourceBindingTargetNameKind(parameter) {
  return String(parameter?.ui_config?.target_name_kind || 'file')
}

export function resourceBindingNameParam(parameter) {
  return getResourceBinding(parameter)?.name_param || ''
}

export function resourceBindingGeometryColumnParam(parameter) {
  return getResourceBinding(parameter)?.geometry_column_param || ''
}

export function geometryColumnFactsFromSelection(selection) {
  const spatial = selection?.raw?.node?.metadata?.capabilities?.spatial || {}
  const columns = (spatial.geometry_columns || [])
    .map(column => typeof column === 'string' ? column : column?.name)
    .filter(Boolean)
  const uniqueColumns = [...new Set(columns)]
  return {
    columns: uniqueColumns,
    selected: uniqueColumns[0] || ''
  }
}

export function resourceBindingInitialLocator(parameter, formData) {
  const binding = getResourceBinding(parameter)
  if (!binding) return ''
  const paramName = binding.mode === 'target' ? binding.parent_locator_param : binding.locator_param
  return paramName ? (formData[paramName] || '') : ''
}

export function applyResourceBindingSelection(parameter, formData, locator, resourceType) {
  const binding = getResourceBinding(parameter)
  if (!binding) return { ...formData }

  const next = { ...formData }
  const locatorParam = binding.mode === 'target' ? binding.parent_locator_param : binding.locator_param
  if (locatorParam) next[locatorParam] = locator || null

  if (binding.type_param) {
    next[binding.type_param] = binding.type_values?.[String(resourceType || '').toLowerCase()] || null
  }
  if (binding.mode === 'target' && binding.name_param && next[binding.name_param] == null) {
    next[binding.name_param] = ''
  }
  for (const [name, value] of Object.entries(binding.default_params || {})) {
    if (next[name] === undefined || next[name] === null || next[name] === '') next[name] = value
  }
  return next
}

export function clearResourceBindingSelection(parameter, formData) {
  const binding = getResourceBinding(parameter)
  if (!binding) return { ...formData }

  const next = { ...formData }
  for (const name of [binding.locator_param, binding.parent_locator_param, binding.name_param, binding.type_param, binding.geometry_column_param]) {
    if (name) next[name] = null
  }
  return next
}

export function missingResourceBindingParams(parameters, formData) {
  const missing = []
  for (const parameter of parameters || []) {
    const binding = getResourceBinding(parameter)
    if (!binding) continue
    const locatorParam = binding.mode === 'target' ? binding.parent_locator_param : binding.locator_param
    if (locatorParam && !formData[locatorParam]) missing.push(locatorParam)
    if (binding.mode === 'target' && binding.name_param && !formData[binding.name_param]) missing.push(binding.name_param)
  }
  return [...new Set(missing)]
}

export function collectResourceBindingParams(parameters, formData) {
  const result = {}
  for (const parameter of parameters || []) {
    const binding = getResourceBinding(parameter)
    if (!binding) continue
    for (const name of [binding.locator_param, binding.parent_locator_param, binding.name_param, binding.type_param, binding.geometry_column_param]) {
      if (name && formData[name] !== undefined && formData[name] !== null && formData[name] !== '') {
        result[name] = formData[name]
      }
    }
  }
  return result
}
