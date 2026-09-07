import { normalizeFieldType } from '../../../../../common-frontend/basic/src/utils/fieldTypes.js'

function isInteger(value) {
  return Number.isInteger(value)
}

function isDecimalType(value) {
  return normalizeFieldType(value) === 'decimal'
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

export function decimalTableWriteLimits(capabilities) {
  const decimal = capabilities?.limits?.table_write?.decimal
  if (!decimal || typeof decimal !== 'object') return null
  const rawMaxPrecision = decimal.max_precision
  const rawMaxScale = decimal.max_scale
  const maxPrecision = rawMaxPrecision === undefined || rawMaxPrecision === null ? null : Number(rawMaxPrecision)
  const maxScale = rawMaxScale === undefined || rawMaxScale === null ? null : Number(rawMaxScale)
  const limits = {
    requiresExplicitPrecisionScale: decimal.requires_explicit_precision_scale === true,
    maxPrecision: isInteger(maxPrecision) && maxPrecision > 0 ? maxPrecision : null,
    maxScale: isInteger(maxScale) && maxScale >= 0 ? maxScale : null
  }
  return limits.requiresExplicitPrecisionScale || limits.maxPrecision !== null || limits.maxScale !== null
    ? limits
    : null
}

function decimalIssueCode(facts, limits) {
  if (!isInteger(facts.precision)) {
    return limits.requiresExplicitPrecisionScale || isInteger(facts.scale) ? 'precision_required' : ''
  }
  if (facts.precision <= 0) return 'precision_invalid'
  if (limits.maxPrecision !== null && facts.precision > limits.maxPrecision) return 'precision_exceeds_max'
  if (!isInteger(facts.scale)) return 'scale_required'
  if (facts.scale < 0) return 'scale_invalid'
  if (limits.maxScale !== null && facts.scale > limits.maxScale) return 'scale_exceeds_max'
  if (facts.scale > facts.precision) return 'scale_exceeds_precision'
  return ''
}

export function decimalMappingIssues(
  mappings,
  sourceFields,
  targetFields,
  targetCapabilities,
  targetRepresentation
) {
  const limits = decimalTableWriteLimits(targetCapabilities)
  if (!limits || String(targetRepresentation || '').toLowerCase() !== 'native') return []

  return (Array.isArray(mappings) ? mappings : []).flatMap((mapping, index) => {
    if (!isDecimalType(mapping?.target_type)) return []
    const targetField = matchingField(targetFields, mapping?.target_field)
    const noManualFacts = !isInteger(mapping?.precision) && !isInteger(mapping?.scale)
    let code = decimalIssueCode(effectiveDecimalFacts(mapping, sourceFields, targetFields), limits)
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

export function decimalMappingsValid(
  mappings,
  sourceFields,
  targetCapabilities,
  targetRepresentation,
  targetFields = []
) {
  return decimalMappingIssues(
    mappings,
    sourceFields,
    targetFields,
    targetCapabilities,
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
