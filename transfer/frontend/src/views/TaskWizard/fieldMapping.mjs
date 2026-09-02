import { decimalFactsFromField, withSourceDecimalFacts } from './decimalMapping.mjs'

import { normalizeFieldType } from '../../../../../common-frontend/basic/src/utils/fieldTypes.js'

function normalizedFieldName(value) {
  return String(value || '').trim().toLowerCase()
}

export function applyTargetFieldDefinition(mapping, targetField) {
  if (!targetField?.name) return mapping

  const targetType = normalizeFieldType(targetField) || mapping.target_type || 'string'
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

export function buildAutomaticFieldMappings(sourceFields, targetFields = []) {
  const mappings = (Array.isArray(sourceFields) ? sourceFields : []).map(field => ({
    source_field: field?.name,
    target_field: field?.name,
    target_type: normalizeFieldType(field),
    ...decimalFactsFromField(field),
    format: '',
    default_value: '',
    nullable: field?.nullable !== false
  }))
  return applyExistingTargetFields(mappings, targetFields)
}

export function reconcileQueryOutputMappings(outputFields, existingMappings, targetFields = []) {
  const fields = Array.isArray(outputFields) ? outputFields : []
  const existing = Array.isArray(existingMappings) ? existingMappings : []
  const automatic = buildAutomaticFieldMappings(fields, targetFields)
  return fields.map((field, index) => {
    const mapping = existing.find(item => normalizedFieldName(item?.source_field) === normalizedFieldName(field?.name))
    if (!mapping) return automatic[index]
    return withSourceDecimalFacts({ ...mapping, source_field: field.name }, field)
  })
}

export function applySourceFieldNullability(mappings, sourceFields) {
  const fields = Array.isArray(sourceFields) ? sourceFields : []
  return (Array.isArray(mappings) ? mappings : []).map(mapping => {
    const sourceField = fields.find(field => normalizedFieldName(field?.name) === normalizedFieldName(mapping?.source_field))
    return sourceField && typeof sourceField.nullable === 'boolean'
      ? { ...mapping, nullable: sourceField.nullable }
      : mapping
  })
}

export function applyFieldMappingEdit(mapping, sourceField) {
  return withSourceDecimalFacts(mapping, sourceField)
}
