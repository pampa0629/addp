import { normalizeFieldType } from '../../../../../common-frontend/basic/src/utils/fieldTypes.js'

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

export const ORACLE_CDC_FIELD_TYPES = Object.freeze([
  'string',
  'bool',
  'int',
  'bigint',
  'float',
  'double',
  'decimal',
  'timestamp'
])

const DATABASE_CDC_SOURCE_TYPES = Object.freeze(['postgresql', 'mysql', 'oracle'])

export function isKafkaTopicSource(engineType, locator) {
  return String(engineType || '').trim().toLowerCase() === 'kafka' && locatorType(locator) === 'topic'
}

export function isDatabaseTableCDCSource(engineType, locator) {
  return DATABASE_CDC_SOURCE_TYPES.includes(normalizedEngineType(engineType)) && locatorType(locator) === 'table'
}

export function resolveSourceLoadState({
	currentLoadMode,
	oldSourceEmpty,
	sourceChanged,
	kafkaTopic
} = {}) {
	if (kafkaTopic) {
		return { loadMode: 'incremental', runtimeBoundary: 'continuous' }
	}
	if (oldSourceEmpty || sourceChanged) {
		return { loadMode: 'snapshot', runtimeBoundary: 'bounded' }
	}
	if (currentLoadMode === 'cdc') {
		return { loadMode: 'cdc', runtimeBoundary: 'continuous' }
	}
	return {
		loadMode: currentLoadMode === 'incremental' ? 'incremental' : 'snapshot',
		runtimeBoundary: 'bounded'
	}
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
	if (DATABASE_CDC_SOURCE_TYPES.includes(provider) &&
			!databaseCDCCapabilitySupports(databaseCDCCapability, provider, targetEngineType)) {
		reasons.push({ code: 'capabilityUnavailable' })
	}
	if (!DATABASE_CDC_SOURCE_TYPES.includes(provider)) {
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
		.filter(field => !supportedTypes.includes(normalizeFieldType(field)))
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
	return mappingIssueCodes(fieldMappings, sourceKeys, CONTINUOUS_FIELD_TYPES).length === 0
}

export function databaseCDCMappingsValid(fieldMappings, sourceKeys, engineType, sourceFields = []) {
	return databaseCDCMappingIssues(fieldMappings, sourceKeys, engineType, sourceFields).length === 0
}

export function databaseCDCMappingIssues(fieldMappings, sourceKeys, engineType, sourceFields = []) {
	const issues = mappingIssueCodes(fieldMappings, sourceKeys, databaseCDCFieldTypes(engineType))
	const mappings = Array.isArray(fieldMappings) ? fieldMappings : []
	const nullableMismatches = (Array.isArray(sourceFields) ? sourceFields : [])
		.filter(field => typeof field?.nullable === 'boolean')
		.filter(field => {
			const mapping = mappings.find(item => sameName(item?.source_field, field?.name))
			return mapping && (mapping.nullable !== false) !== field.nullable
		})
		.map(field => String(field?.name || '').trim())
		.filter(Boolean)
	if (nullableMismatches.length > 0) {
		issues.push({ code: 'mappingNullableMismatch', fields: nullableMismatches })
	}
	return issues
}

export function databaseCDCFieldTypes(engineType) {
	const provider = normalizedEngineType(engineType)
	if (provider === 'mysql') return MYSQL_CDC_FIELD_TYPES
	if (provider === 'oracle') return ORACLE_CDC_FIELD_TYPES
	return POSTGRESQL_CDC_FIELD_TYPES
}

function mappingIssueCodes(fieldMappings, sourceKeys, supportedTypes) {
  const mappings = Array.isArray(fieldMappings) ? fieldMappings : []
  const keys = normalizedNames(sourceKeys)
  const issues = []
  if (mappings.length === 0) issues.push({ code: 'mappingEmpty' })
  if (keys.length === 0) issues.push({ code: 'keyEmpty' })
  if (issues.length > 0) return issues

  const sourceNames = new Set()
  const targetNames = new Set()
  for (const mapping of mappings) {
    const source = normalizedName(mapping?.source_field)
    const target = normalizedName(mapping?.target_field)
    const targetType = String(mapping?.target_type || '').trim().toLowerCase()
		if (!source || !target) {
			issues.push({ code: 'mappingFieldMissing' })
			continue
		}
		if (!supportedTypes.includes(targetType)) {
			issues.push({ code: 'mappingTypeUnsupported', fields: [String(mapping?.target_field || mapping?.source_field || '').trim()].filter(Boolean) })
		}
		if (sourceNames.has(source)) issues.push({ code: 'mappingSourceDuplicate', fields: [String(mapping?.source_field || '').trim()].filter(Boolean) })
		if (targetNames.has(target)) issues.push({ code: 'mappingTargetDuplicate', fields: [String(mapping?.target_field || '').trim()].filter(Boolean) })
    sourceNames.add(source)
    targetNames.add(target)
  }

  const mappedKeys = continuousMappedTargetKeys(mappings, keys)
  if (mappedKeys.length !== keys.length) issues.push({ code: 'keyMappingMissing' })
	const nullableKeys = keys.filter(sourceKey => {
    const mapping = mappings.find(item => sameName(item?.source_field, sourceKey))
		return mapping && mapping.nullable !== false
  })
	if (nullableKeys.length > 0) issues.push({ code: 'keyNullable', fields: nullableKeys })
	return issues
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
