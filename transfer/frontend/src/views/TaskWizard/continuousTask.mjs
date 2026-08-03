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

export const MYSQL_CDC_FIELD_TYPES = Object.freeze([
  'string',
  'int',
  'bigint',
  'float',
  'double',
  'decimal',
  'date',
  'time',
  'timestamp',
  'json',
  'bytes'
])

export function isKafkaTopicSource(engineType, locator) {
  return String(engineType || '').trim().toLowerCase() === 'kafka' && locatorType(locator) === 'topic'
}

export function isDatabaseTableCDCSource(engineType, locator) {
  return ['postgresql', 'mysql'].includes(normalizedEngineType(engineType)) && locatorType(locator) === 'table'
}

export function databaseCDCUnavailableReasonCodes({
	sourceEngineType,
	sourceLocator,
	sourceRepresentation,
	sourceDataType,
	targetEngineType,
	targetRepresentation,
	sourceFields,
	databaseCDCCapability
} = {}) {
	const reasons = []
	const provider = normalizedEngineType(sourceEngineType)
	if (['postgresql', 'mysql'].includes(provider) &&
			!databaseCDCCapabilitySupports(databaseCDCCapability, provider, targetEngineType)) {
		reasons.push({ code: 'capabilityUnavailable' })
	}
	if (!['postgresql', 'mysql'].includes(provider)) {
		reasons.push({ code: 'sourceDatabaseRequired' })
	} else if (locatorType(sourceLocator) !== 'table' || String(sourceDataType || '').trim().toLowerCase() !== 'table') {
		reasons.push({ code: 'sourceTableRequired' })
	}
	if (String(sourceRepresentation || '').trim().toLowerCase() !== 'native') {
		reasons.push({ code: 'sourceNativeRequired' })
	}
	if (!['postgresql', 'mysql'].includes(normalizedEngineType(targetEngineType))) {
		reasons.push({ code: 'targetAtomicApplyRequired' })
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
	const supportedTypes = databaseCDCFieldTypes(provider)
	const unsupportedFields = fields
		.filter(field => !supportedTypes.includes(String(field?.type || '').trim().toLowerCase()))
		.map(field => String(field?.name || '').trim())
		.filter(Boolean)
	if (unsupportedFields.length > 0) {
		reasons.push({ code: 'sourceFieldTypesUnsupported', fields: unsupportedFields })
	}
	return reasons
}

function databaseCDCCapabilitySupports(capability, sourceType, targetType) {
	if (!capability || typeof capability !== 'object') return false
	const sources = Array.isArray(capability.sources)
			? capability.sources.map(normalizedEngineType)
			: []
	const targets = Array.isArray(capability.targets)
			? capability.targets.map(normalizedEngineType)
			: []
	const bootstrap = Array.isArray(capability.bootstrap)
		? capability.bootstrap.map(value => String(value || '').trim().toLowerCase())
		: []
	return sources.includes(normalizedEngineType(sourceType)) &&
			targets.includes(normalizedEngineType(targetType)) &&
		bootstrap.includes('initial_snapshot') &&
		String(capability.apply_mode || capability.applyMode || '').trim().toLowerCase() === 'upsert_delete'
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

export function databaseCDCMappingsValid(fieldMappings, sourceKeys, engineType) {
	return mappingsValidForTypes(fieldMappings, sourceKeys, databaseCDCFieldTypes(engineType))
}

export function databaseCDCFieldTypes(engineType) {
	return normalizedEngineType(engineType) === 'mysql'
		? MYSQL_CDC_FIELD_TYPES
		: POSTGRESQL_CDC_FIELD_TYPES
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

export function buildDatabaseCDCSourceEndpoint(locator) {
  return {
    locator: String(locator || '').trim(),
    data_type: 'table',
    representation: 'native'
  }
}

function normalizedEngineType(value) {
	const type = String(value || '').trim().toLowerCase()
	if (type.includes('postgres')) return 'postgresql'
	if (type.includes('mysql')) return 'mysql'
	return type
}

export function taskEngineDescriptors(task, engines) {
	const sourceEngineID = locatorEngineID(task?.config?.source?.locator)
	const targetEngineID = locatorEngineID(task?.config?.target?.parent_locator)
	const values = Array.isArray(engines) ? engines : []
	const findDescriptor = engineID => {
		const engine = values.find(candidate => Number(candidate?.id) === Number(engineID))
		if (!engine) return null
		return {
			engine_type: String(engine.engine_type || '').trim(),
			capabilities: engine.capabilities || null
		}
	}
	return {
		source: findDescriptor(sourceEngineID),
		target: findDescriptor(targetEngineID)
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
