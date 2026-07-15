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

export const POSTGRESQL_CDC_FIELD_TYPES = Object.freeze([
  ...CONTINUOUS_FIELD_TYPES,
  'geometry'
])

export function isKafkaTopicSource(engineType, locator) {
  return String(engineType || '').trim().toLowerCase() === 'kafka' && locatorType(locator) === 'topic'
}

export function isPostgreSQLTableSource(engineType, locator) {
  return String(engineType || '').trim().toLowerCase() === 'postgresql' && locatorType(locator) === 'table'
}

export function postgresqlCDCUnavailableReasonCodes({
	sourceEngineType,
	sourceLocator,
	sourceRepresentation,
	sourceDataType,
	targetEngineType,
	targetRepresentation,
	sourceFields
} = {}) {
	const reasons = []
	if (String(sourceEngineType || '').trim().toLowerCase() !== 'postgresql') {
		reasons.push({ code: 'sourcePostgreSQLRequired' })
	} else if (locatorType(sourceLocator) !== 'table' || String(sourceDataType || '').trim().toLowerCase() !== 'table') {
		reasons.push({ code: 'sourceTableRequired' })
	}
	if (String(sourceRepresentation || '').trim().toLowerCase() !== 'native') {
		reasons.push({ code: 'sourceNativeRequired' })
	}
	if (String(targetEngineType || '').trim().toLowerCase() !== 'postgresql') {
		reasons.push({ code: 'targetPostgreSQLRequired' })
	}
	if (String(targetRepresentation || '').trim().toLowerCase() !== 'native') {
		reasons.push({ code: 'targetNativeRequired' })
	}
	const fields = Array.isArray(sourceFields) ? sourceFields : []
	if (fields.length === 0) {
		reasons.push({ code: 'sourceFieldsRequired' })
		return reasons
	}
	if (!fields.some(isPrimaryKeyField)) {
		reasons.push({ code: 'sourcePrimaryKeyRequired' })
	}
	const unsupportedFields = fields
		.filter(field => !POSTGRESQL_CDC_FIELD_TYPES.includes(String(field?.type || '').trim().toLowerCase()))
		.map(field => String(field?.name || '').trim())
		.filter(Boolean)
	if (unsupportedFields.length > 0) {
		reasons.push({ code: 'sourceFieldTypesUnsupported', fields: unsupportedFields })
	}
	return reasons
}

export function continuousMappedTargetKeys(fieldMappings, sourceKeys) {
  const mappings = Array.isArray(fieldMappings) ? fieldMappings : []
  return normalizedNames(sourceKeys).map(sourceKey => {
    const mapping = mappings.find(item => sameName(item?.source_field, sourceKey))
    return String(mapping?.target_field || '').trim()
  }).filter(Boolean)
}

export function normalizeContinuousKeyFields(sourceKeys, fieldMappings) {
	const mappedSources = normalizedNames(
		(Array.isArray(fieldMappings) ? fieldMappings : []).map(mapping => mapping?.source_field)
	)
	return normalizedNames(sourceKeys).filter(key => mappedSources.some(source => sameName(source, key)))
}

export function continuousMappingsValid(fieldMappings, sourceKeys) {
	return mappingsValidForTypes(fieldMappings, sourceKeys, CONTINUOUS_FIELD_TYPES)
}

export function postgresqlCDCMappingsValid(fieldMappings, sourceKeys) {
	return mappingsValidForTypes(fieldMappings, sourceKeys, POSTGRESQL_CDC_FIELD_TYPES)
}

function mappingsValidForTypes(fieldMappings, sourceKeys, supportedTypes) {
  const mappings = Array.isArray(fieldMappings) ? fieldMappings : []
  const keys = normalizedNames(sourceKeys)
  if (mappings.length === 0 || keys.length === 0) return false

  const sourceNames = new Set()
  const targetNames = new Set()
  for (const mapping of mappings) {
    const source = normalizedName(mapping?.source_field)
    const target = normalizedName(mapping?.target_field)
    const targetType = String(mapping?.target_type || '').trim().toLowerCase()
		if (!source || !target || !supportedTypes.includes(targetType)) return false
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

export function taskEngineTypes(task, engines) {
	const sourceEngineID = locatorEngineID(task?.config?.source?.locator)
	const targetEngineID = locatorEngineID(task?.config?.target?.parent_locator)
	const values = Array.isArray(engines) ? engines : []
	const findType = engineID => String(
		values.find(engine => Number(engine?.id) === Number(engineID))?.engine_type || ''
	).trim()
	return {
		source: findType(sourceEngineID),
		target: findType(targetEngineID)
	}
}

function locatorType(locator) {
  try {
    return String(new URL(String(locator || '')).searchParams.get('type') || '').trim().toLowerCase()
  } catch {
    return ''
  }
}

function locatorEngineID(locator) {
	try {
		const match = String(locator || '').match(/^addp:\/\/engine\/(\d+)(?:\/|$)/)
		return match ? Number(match[1]) : 0
	} catch {
		return 0
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

function isPrimaryKeyField(field) {
	return field?.primary_key === true ||
		field?.primaryKey === true ||
		field?.is_primary_key === true ||
		String(field?.key || '').trim().toLowerCase() === 'pri'
}
