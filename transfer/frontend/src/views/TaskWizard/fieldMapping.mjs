import { decimalFactsFromField, withSourceDecimalFacts } from './decimalMapping.mjs'

function normalizedFieldName(value) {
  return String(value || '').trim().toLowerCase()
}

export function applyTargetFieldDefinition(mapping, targetField) {
  if (!targetField?.name) return mapping

  const targetType = normalizedFieldName(targetField.type) || mapping.target_type || 'string'
  const next = {
    ...mapping,
    target_field: targetField.name,
    target_type: targetType
  }
  delete next.precision
  delete next.scale
  if (targetType === 'decimal') {
    Object.assign(next, decimalFactsFromField(targetField))
  }
  return next
}

export function applyExistingTargetFields(mappings, targetFields) {
  const fields = Array.isArray(targetFields) ? targetFields : []
  return (Array.isArray(mappings) ? mappings : []).map(mapping => {
    const targetName = normalizedFieldName(mapping?.target_field)
    const targetField = fields.find(field => normalizedFieldName(field?.name) === targetName)
    return targetField ? applyTargetFieldDefinition(mapping, targetField) : mapping
  })
}

export function applyFieldMappingEdit(mapping, sourceField) {
  return withSourceDecimalFacts(mapping, sourceField)
}
