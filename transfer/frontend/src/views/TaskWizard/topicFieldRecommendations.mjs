import { applyExistingTargetFields } from './fieldMapping.mjs'

function isJSONObject(value) {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

function inferredValueType(value) {
  if (value === null || value === undefined) return ''
  if (typeof value === 'boolean') return 'bool'
  if (typeof value === 'number') return Number.isInteger(value) ? 'bigint' : 'double'
  if (typeof value === 'string') return 'string'
  return 'json'
}

function mergedValueType(current, next) {
  if (!current) return next
  if (!next || current === next) return current
  if ([current, next].every(type => ['bigint', 'double'].includes(type))) return 'double'
  return 'json'
}

function normalizedName(value) {
  return String(value || '').trim().toLowerCase()
}

function isBlankMapping(mapping) {
  return !String(mapping?.source_field || '').trim() && !String(mapping?.target_field || '').trim()
}

export function inferTopicFieldRecommendations(rows) {
  const samples = (Array.isArray(rows) ? rows : [])
    .map(row => row?.value)
    .filter(isJSONObject)
  if (samples.length === 0) return []

  const fields = new Map()
  for (const sample of samples) {
    for (const [name, value] of Object.entries(sample)) {
      if (!name.trim()) continue
      const existing = fields.get(name) || {
        name,
        type: '',
        nullable: false,
        present_count: 0,
        sample_count: samples.length
      }
      existing.present_count += 1
      existing.nullable ||= value === null || value === undefined
      existing.type = mergedValueType(existing.type, inferredValueType(value))
      fields.set(name, existing)
    }
  }

  return Array.from(fields.values()).map(field => ({
    ...field,
    type: field.type || 'json',
    nullable: field.nullable || field.present_count < field.sample_count
  }))
}

export function mergeTopicFieldRecommendations({
  recommendations,
  sourceFields,
  fieldMappings,
  targetFields
} = {}) {
  const nextSourceFields = [...(Array.isArray(sourceFields) ? sourceFields : [])]
  const nextMappings = (Array.isArray(fieldMappings) ? fieldMappings : []).filter(mapping => !isBlankMapping(mapping))
  const fields = Array.isArray(targetFields) ? targetFields : []
  let addedCount = 0

  for (const recommendation of Array.isArray(recommendations) ? recommendations : []) {
    const sourceName = String(recommendation?.name || '').trim()
    const targetName = String(recommendation?.target_name || recommendation?.target_field || sourceName).trim()
    const targetType = String(recommendation?.type || '').trim().toLowerCase()
    if (!sourceName || !targetName || !targetType) continue

    if (!nextSourceFields.some(field => normalizedName(field?.name) === normalizedName(sourceName))) {
      nextSourceFields.push({
        name: sourceName,
        type: targetType,
        nullable: recommendation?.nullable !== false
      })
    }
    if (nextMappings.some(mapping => normalizedName(mapping?.source_field) === normalizedName(sourceName))) {
      continue
    }

    const targetField = fields.find(field => normalizedName(field?.name) === normalizedName(targetName))
    const [mapping] = applyExistingTargetFields([{
      source_field: sourceName,
      target_field: targetName,
      target_type: targetType,
      format: '',
      default_value: '',
      nullable: recommendation?.nullable !== false
    }], fields)
    if (targetField && typeof targetField.nullable === 'boolean') {
      mapping.nullable = targetField.nullable
    }
    nextMappings.push(mapping)
    addedCount += 1
  }

  return {
    sourceFields: nextSourceFields,
    fieldMappings: nextMappings,
    addedCount
  }
}

