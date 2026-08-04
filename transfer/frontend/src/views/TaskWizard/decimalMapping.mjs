function isInteger(value) {
  return Number.isInteger(value)
}

function isDecimalType(value) {
  return String(value || '').trim().toLowerCase() === 'decimal'
}

export function decimalFactsFromField(field) {
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
  if (isInteger(next.precision) || isInteger(next.scale)) return next
  return { ...next, ...decimalFactsFromField(sourceField) }
}

function matchingField(fields, name) {
  const normalizedName = String(name || '').trim().toLowerCase()
  return (Array.isArray(fields) ? fields : []).find(field =>
    String(field?.name || '').trim().toLowerCase() === normalizedName
  )
}

function effectiveDecimalFacts(mapping, sourceFields, targetFields) {
  const targetField = matchingField(targetFields, mapping?.target_field)
  const targetFacts = decimalFactsFromField(targetField)
  const sourceField = matchingField(sourceFields, mapping?.source_field)
  const sourceFacts = decimalFactsFromField(sourceField)
  return {
    precision: isInteger(mapping?.precision)
      ? mapping.precision
      : (targetFacts.precision ?? sourceFacts.precision),
    scale: isInteger(mapping?.scale)
      ? mapping.scale
      : (targetFacts.scale ?? sourceFacts.scale)
  }
}

function isMySQLNativeTarget(targetEngineType, targetRepresentation) {
  return String(targetEngineType || '').toLowerCase().includes('mysql') &&
    String(targetRepresentation || '').toLowerCase() === 'native'
}

function decimalIssueCode(facts) {
  if (!isInteger(facts.precision)) return 'precision_required'
  if (facts.precision <= 0 || facts.precision > 65) return 'precision_out_of_range'
  if (!isInteger(facts.scale)) return 'scale_required'
  if (facts.scale < 0 || facts.scale > 30) return 'scale_out_of_range'
  if (facts.scale > facts.precision) return 'scale_exceeds_precision'
  return ''
}

export function mysqlDecimalMappingIssues(
  mappings,
  sourceFields,
  targetFields,
  targetEngineType,
  targetRepresentation
) {
  if (!isMySQLNativeTarget(targetEngineType, targetRepresentation)) return []

  return (Array.isArray(mappings) ? mappings : []).flatMap((mapping, index) => {
    if (!isDecimalType(mapping?.target_type)) return []
    const targetField = matchingField(targetFields, mapping?.target_field)
    const noManualFacts = !isInteger(mapping?.precision) && !isInteger(mapping?.scale)
    let code = decimalIssueCode(effectiveDecimalFacts(mapping, sourceFields, targetFields))
    if (
      code === 'precision_required' &&
      noManualFacts &&
      isDecimalType(targetField?.type) &&
      !isInteger(decimalFactsFromField(targetField).precision)
    ) {
      code = 'target_definition_missing'
    }
    if (!code) return []
    return [{
      index,
      sourceField: String(mapping?.source_field || '').trim(),
      targetField: String(mapping?.target_field || '').trim(),
      code
    }]
  })
}

export function mysqlDecimalMappingsValid(
  mappings,
  sourceFields,
  targetEngineType,
  targetRepresentation,
  targetFields = []
) {
  return mysqlDecimalMappingIssues(
    mappings,
    sourceFields,
    targetFields,
    targetEngineType,
    targetRepresentation
  ).length === 0
}

export function applyDecimalRecommendations(mappings, recommendations) {
  const bySource = new Map((Array.isArray(recommendations) ? recommendations : []).map(item => [
    String(item?.source_field || '').trim().toLowerCase(),
    item
  ]))
  return (Array.isArray(mappings) ? mappings : []).map(mapping => {
    if (!isDecimalType(mapping?.target_type)) return mapping
    const recommendation = bySource.get(String(mapping?.source_field || '').trim().toLowerCase())
    if (!recommendation || !isInteger(recommendation.precision) || !isInteger(recommendation.scale)) return mapping
    return { ...mapping, precision: recommendation.precision, scale: recommendation.scale }
  })
}
