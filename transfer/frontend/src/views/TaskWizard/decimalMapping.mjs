function isInteger(value) {
  return Number.isInteger(value)
}

function isDecimalType(value) {
  return String(value || '').trim().toLowerCase() === 'decimal'
}

export function decimalFactsFromSourceField(field) {
  if (!isDecimalType(field?.type)) return {}
  const precision = Number(field?.precision)
  const scale = Number(field?.scale)
  if (!isInteger(precision) || precision <= 0 || !isInteger(scale) || scale < 0 || scale > precision) {
    return {}
  }
  return { precision, scale }
}

export function withSourceDecimalFacts(mapping, sourceField) {
  const next = { ...mapping }
  if (!isDecimalType(next.target_type)) {
    delete next.precision
    delete next.scale
    return next
  }
  if (isInteger(next.precision) && isInteger(next.scale)) return next
  return { ...next, ...decimalFactsFromSourceField(sourceField) }
}

export function mysqlDecimalMappingsValid(mappings, sourceFields, targetEngineType, targetRepresentation) {
  const isMySQLNativeTarget = String(targetEngineType || '').toLowerCase().includes('mysql') &&
    String(targetRepresentation || '').toLowerCase() === 'native'
  if (!isMySQLNativeTarget) return true

  const fields = Array.isArray(sourceFields) ? sourceFields : []
  return (Array.isArray(mappings) ? mappings : []).every(mapping => {
    if (!isDecimalType(mapping?.target_type)) return true
    const sourceName = String(mapping?.source_field || '').trim().toLowerCase()
    const sourceField = fields.find(field => String(field?.name || '').trim().toLowerCase() === sourceName)
    const effective = withSourceDecimalFacts(mapping, sourceField)
    return isInteger(effective.precision) && effective.precision > 0 && effective.precision <= 65 &&
      isInteger(effective.scale) && effective.scale >= 0 && effective.scale <= 30 &&
      effective.scale <= effective.precision
  })
}
