export const CONTINUOUS_FIELD_TYPES = Object.freeze([
  'string',
  'bool',
  'int',
  'bigint',
  'float',
  'double',
  'decimal',
  'date',
  'time',
  'timestamp',
  'json',
  'uuid'
])

export function isKafkaTopicSource(engineType, locator) {
  return String(engineType || '').trim().toLowerCase() === 'kafka' && locatorType(locator) === 'topic'
}

export function isPostgreSQLTableSource(engineType, locator) {
  return String(engineType || '').trim().toLowerCase() === 'postgresql' && locatorType(locator) === 'table'
}

export function continuousMappedTargetKeys(fieldMappings, sourceKeys) {
  const mappings = Array.isArray(fieldMappings) ? fieldMappings : []
  return normalizedNames(sourceKeys).map(sourceKey => {
    const mapping = mappings.find(item => sameName(item?.source_field, sourceKey))
    return String(mapping?.target_field || '').trim()
  }).filter(Boolean)
}

export function continuousMappingsValid(fieldMappings, sourceKeys) {
  const mappings = Array.isArray(fieldMappings) ? fieldMappings : []
  const keys = normalizedNames(sourceKeys)
  if (mappings.length === 0 || keys.length === 0) return false

  const sourceNames = new Set()
  const targetNames = new Set()
  for (const mapping of mappings) {
    const source = normalizedName(mapping?.source_field)
    const target = normalizedName(mapping?.target_field)
    const targetType = String(mapping?.target_type || '').trim().toLowerCase()
    if (!source || !target || !CONTINUOUS_FIELD_TYPES.includes(targetType)) return false
    if (sourceNames.has(source) || targetNames.has(target)) return false
    sourceNames.add(source)
    targetNames.add(target)
  }

  const mappedKeys = continuousMappedTargetKeys(mappings, keys)
  if (mappedKeys.length !== keys.length) return false
  return keys.every(sourceKey => {
    const mapping = mappings.find(item => sameName(item?.source_field, sourceKey))
    return mapping?.nullable === false
  })
}

export function cdcMappingsCoverSourceFields(fieldMappings, sourceFields) {
	const mapped = normalizedNames((Array.isArray(fieldMappings) ? fieldMappings : []).map(item => item?.source_field))
	const source = normalizedNames((Array.isArray(sourceFields) ? sourceFields : []).map(item => item?.name))
	if (mapped.length === 0 || mapped.length !== source.length) return false
	return source.every(field => mapped.some(mappedField => sameName(mappedField, field)))
}

export function buildContinuousSourceEndpoint(locator, sourceKeys, initialPosition, pollBatchSize) {
  return {
    locator: String(locator || '').trim(),
    representation: 'native',
    change_stream: {
      envelope: 'record',
      encoding: 'json',
      key: {
        source: 'value',
        fields: normalizedNames(sourceKeys)
      },
      start: {
        mode: 'committed',
        initial: initialPosition === 'latest' ? 'latest' : 'earliest'
      },
      poll_batch_size: Number(pollBatchSize)
    }
  }
}

export function buildPostgreSQLCDCSourceEndpoint(locator) {
  return {
    locator: String(locator || '').trim(),
    data_type: 'table',
    representation: 'native'
  }
}

function locatorType(locator) {
  try {
    return String(new URL(String(locator || '')).searchParams.get('type') || '').trim().toLowerCase()
  } catch {
    return ''
  }
}

function normalizedNames(values) {
  const seen = new Set()
  return (Array.isArray(values) ? values : [])
    .map(value => String(value || '').trim())
    .filter(value => {
      const key = normalizedName(value)
      if (!key || seen.has(key)) return false
      seen.add(key)
      return true
    })
}

function normalizedName(value) {
  return String(value || '').trim().toLowerCase()
}

function sameName(left, right) {
  return normalizedName(left) === normalizedName(right)
}
