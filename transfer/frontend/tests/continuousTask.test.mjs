import test from 'node:test'
import assert from 'node:assert/strict'

import {
  buildContinuousSourceEndpoint,
	buildDatabaseCDCSourceEndpoint,
	cdcMappingsCoverSourceFields,
	continuousMappedTargetKeys,
	continuousMappingsValid,
	databaseCDCMappingIssues,
	databaseCDCMappingsValid,
	databaseCDCUnavailableReasonCodes,
	isKafkaTopicSource,
	isDatabaseTableCDCSource,
	normalizeContinuousKeyFields,
	resolveSourceLoadState,
	taskEngineDescriptors
} from '../src/views/TaskWizard/continuousTask.mjs'

const databaseCDCCapability = {
		sources: ['postgresql', 'mysql', 'oracle'],
		targets: ['postgresql', 'mysql', 'oracle'],
	bootstrap: ['initial_snapshot'],
	apply_mode: 'upsert_delete'
}

test('Kafka topic source is the only continuous source shape', () => {
  assert.equal(isKafkaTopicSource('kafka', 'addp://engine/30/path/orders.events?type=topic'), true)
  assert.equal(isKafkaTopicSource('postgresql', 'addp://engine/30/path/orders.events?type=topic'), false)
  assert.equal(isKafkaTopicSource('kafka', 'addp://engine/30/path/orders?type=table'), false)
})

test('PostgreSQL, MySQL and Oracle tables use the single database CDC source shape', () => {
	assert.equal(isDatabaseTableCDCSource('postgresql', 'addp://engine/12/path/public/orders?type=table'), true)
	assert.equal(isDatabaseTableCDCSource('mysql', 'addp://engine/13/path/business/orders?type=table'), true)
	assert.equal(isDatabaseTableCDCSource('oracle', 'addp://engine/22/path/BUSINESS/CUSTOMERS?type=table'), true)
	assert.equal(isDatabaseTableCDCSource('kafka', 'addp://engine/12/path/public/orders?type=table'), false)
	assert.deepEqual(buildDatabaseCDCSourceEndpoint('addp://engine/12/path/public/orders?type=table'), {
		locator: 'addp://engine/12/path/public/orders?type=table',
		data_type: 'table',
		representation: 'native'
	})
})

test('reselecting the same database CDC source preserves the continuous runtime', () => {
	assert.deepEqual(resolveSourceLoadState({
		currentLoadMode: 'cdc',
		oldSourceEmpty: false,
		sourceChanged: false,
		kafkaTopic: false
	}), { loadMode: 'cdc', runtimeBoundary: 'continuous' })
	assert.deepEqual(resolveSourceLoadState({
		currentLoadMode: 'cdc',
		oldSourceEmpty: false,
		sourceChanged: true,
		kafkaTopic: false
	}), { loadMode: 'snapshot', runtimeBoundary: 'bounded' })
})

test('continuous target keys follow source key mapping order', () => {
  const mappings = [
    { source_field: 'tenant_id', target_field: 'tenant_id', target_type: 'bigint', nullable: false },
    { source_field: 'id', target_field: 'order_id', target_type: 'int', nullable: false },
    { source_field: 'name', target_field: 'name', target_type: 'string', nullable: true }
  ]
  assert.deepEqual(continuousMappedTargetKeys(mappings, ['id', 'tenant_id']), ['order_id', 'tenant_id'])
  assert.equal(continuousMappingsValid(mappings, ['id', 'tenant_id']), true)
})

test('continuous key normalization is idempotent and removes only unmapped keys', () => {
	const mappings = [
		{ source_field: 'id' },
		{ source_field: 'tenant_id' }
	]
	const first = normalizeContinuousKeyFields(['ID', 'missing', 'tenant_id', 'id'], mappings)
	const second = normalizeContinuousKeyFields(first, mappings)
	assert.deepEqual(first, ['ID', 'tenant_id'])
	assert.deepEqual(second, first)
})

test('continuous source endpoint uses the strict v1 contract', () => {
  assert.deepEqual(buildContinuousSourceEndpoint(
    'addp://engine/30/path/orders.events?type=topic',
    ['tenant_id', 'id'],
    'latest',
    500
  ), {
    locator: 'addp://engine/30/path/orders.events?type=topic',
    representation: 'native',
    change_stream: {
      envelope: 'record',
      encoding: 'json',
      key: { source: 'value', fields: ['tenant_id', 'id'] },
      start: { mode: 'committed', initial: 'latest' },
      poll_batch_size: 500
    }
  })
})

test('continuous mapping rejects nullable keys, duplicate fields and unsupported types', () => {
  assert.equal(continuousMappingsValid([
    { source_field: 'id', target_field: 'id', target_type: 'int', nullable: true }
  ], ['id']), false)
  assert.equal(continuousMappingsValid([
    { source_field: 'id', target_field: 'id', target_type: 'integer', nullable: false }
  ], ['id']), false)
  assert.equal(continuousMappingsValid([
    { source_field: 'id', target_field: 'id', target_type: 'int', nullable: false },
    { source_field: 'ID', target_field: 'other_id', target_type: 'int', nullable: false }
  ], ['id']), false)
})

test('database CDC mapping diagnostics identify the exact invalid key condition', () => {
	assert.deepEqual(databaseCDCMappingIssues([
		{ source_field: 'id', target_field: 'id', target_type: 'bigint', nullable: true }
	], ['id'], 'postgresql'), [{ code: 'keyNullable', fields: ['id'] }])
	assert.deepEqual(databaseCDCMappingIssues([
		{ source_field: 'id', target_field: 'id', target_type: 'bigint', nullable: false },
		{ source_field: 'shape', target_field: 'shape', target_type: 'geometry', nullable: true }
	], ['id'], 'mysql'), [{ code: 'mappingTypeUnsupported', fields: ['shape'] }])
})

test('database CDC mapping diagnostics reject nullability drift on non-key fields', () => {
	assert.deepEqual(databaseCDCMappingIssues([
		{ source_field: 'id', target_field: 'id', target_type: 'bigint', nullable: false },
		{ source_field: 'geometry', target_field: 'geometry', target_type: 'geometry', nullable: true }
	], ['id'], 'postgresql', [
		{ name: 'id', nullable: false },
		{ name: 'geometry', nullable: false }
	]), [{ code: 'mappingNullableMismatch', fields: ['geometry'] }])
})

test('CDC mapping must cover the complete frozen source schema', () => {
	const fields = [{ name: 'id' }, { name: 'name' }]
	assert.equal(cdcMappingsCoverSourceFields([
		{ source_field: 'id' }, { source_field: 'name' }
	], fields), true)
	assert.equal(cdcMappingsCoverSourceFields([{ source_field: 'id' }], fields), false)
})

test('database CDC mapping types are provider-specific', () => {
	const mappings = [
		{ source_field: 'id', target_field: 'id', target_type: 'bigint', nullable: false },
		{ source_field: 'shape', target_field: 'geometry', target_type: 'geometry', nullable: true }
	]
	assert.equal(databaseCDCMappingsValid(mappings, ['id'], 'postgresql'), true)
	assert.equal(databaseCDCMappingsValid(mappings, ['id'], 'mysql'), false)
	assert.equal(databaseCDCMappingsValid(mappings, ['id'], 'oracle'), true)
	assert.equal(continuousMappingsValid(mappings, ['id']), false)
	assert.equal(databaseCDCMappingsValid([
		{ source_field: 'id', target_field: 'id', target_type: 'bigint', nullable: false },
		{ source_field: 'payload', target_field: 'payload', target_type: 'bytes', nullable: false }
	], ['id'], 'mysql'), true)
	assert.equal(databaseCDCMappingsValid([
		{ source_field: 'ID', target_field: 'id', target_type: 'bigint', nullable: false },
		{ source_field: 'CREATED_AT', target_field: 'created_at', target_type: 'timestamp', nullable: false }
	], ['ID'], 'oracle'), true)
	assert.equal(databaseCDCMappingsValid([
		{ source_field: 'ID', target_field: 'id', target_type: 'bigint', nullable: false },
		{ source_field: 'PAYLOAD', target_field: 'payload', target_type: 'bytes', nullable: true }
	], ['ID'], 'oracle'), false)
})

test('database CDC availability reports provider-specific blocking reasons', () => {
	const available = databaseCDCUnavailableReasonCodes({
		sourceEngineType: 'postgresql',
		sourceLocator: 'addp://engine/8/path/public/roads?type=table',
		sourceRepresentation: 'native',
		sourceDataType: 'table',
		targetEngineType: 'postgresql',
		targetRepresentation: 'native',
		sourceFields: [
			{ name: 'id', type: 'bigint', primary_key: true },
			{ name: 'shape', type: 'geometry' }
		],
		databaseCDCCapability
	})
	assert.deepEqual(available, [])
	assert.deepEqual(databaseCDCUnavailableReasonCodes({
		sourceEngineType: 'mysql',
		sourceLocator: 'addp://engine/9/path/business/orders?type=table',
		sourceRepresentation: 'native',
		sourceDataType: 'table',
		targetEngineType: 'postgresql',
		targetRepresentation: 'native',
		sourceFields: [
			{ name: 'id', type: 'bigint', primary_key: true },
			{ name: 'payload', type: 'bytes' }
		],
			databaseCDCCapability
		}), [])
	assert.deepEqual(databaseCDCUnavailableReasonCodes({
		sourceEngineType: 'postgresql',
		sourceLocator: 'addp://engine/8/path/public/orders?type=table',
		sourceRepresentation: 'native',
		sourceDataType: 'table',
		targetEngineType: 'mysql',
		targetRepresentation: 'native',
		sourceFields: [{ name: 'id', type: 'bigint', primary_key: true }],
		databaseCDCCapability
	}), [])
	assert.deepEqual(databaseCDCUnavailableReasonCodes({
		sourceEngineType: 'oracle',
		sourceLocator: 'addp://engine/22/path/BUSINESS/CUSTOMERS?type=table',
		sourceRepresentation: 'native',
		sourceDataType: 'table',
		targetEngineType: 'postgresql',
		targetRepresentation: 'native',
		sourceFields: [
			{ name: 'ID', type: 'bigint', primary_key: true },
			{ name: 'CREATED_AT', type: 'timestamp' }
		],
		databaseCDCCapability
	}), [])

	const unavailable = databaseCDCUnavailableReasonCodes({
		sourceEngineType: 'kafka',
		sourceLocator: 'addp://engine/9/path/public/roads?type=table',
		sourceRepresentation: 'encoded',
		sourceDataType: 'table',
		targetEngineType: 'minio',
		targetRepresentation: 'encoded',
		sourceFields: [{ name: 'payload', type: 'bytes' }],
		databaseCDCCapability
	})
	assert.deepEqual(unavailable.map(reason => reason.code), [
		'sourceDatabaseRequired',
		'sourceNativeRequired',
			'targetAtomicApplyRequired',
		'targetNativeRequired',
		'sourcePrimaryKeyRequired',
		'sourceFieldTypesUnsupported'
	])
	assert.deepEqual(unavailable.at(-1).fields, ['payload'])

	const mysqlUnsupported = databaseCDCUnavailableReasonCodes({
		sourceEngineType: 'mysql',
		sourceLocator: 'addp://engine/9/path/business/orders?type=table',
		sourceRepresentation: 'native',
		sourceDataType: 'table',
		targetEngineType: 'postgresql',
		targetRepresentation: 'native',
		sourceFields: [
			{ name: 'id', type: 'bigint', primary_key: true },
			{ name: 'enabled', type: 'bool' }
		],
		databaseCDCCapability
	})
	assert.deepEqual(mysqlUnsupported, [{ code: 'sourceFieldTypesUnsupported', fields: ['enabled'] }])
	assert.deepEqual(databaseCDCUnavailableReasonCodes({
		sourceEngineType: 'mysql',
		sourceLocator: 'addp://engine/9/path/business/orders?type=table',
		sourceRepresentation: 'native',
		sourceDataType: 'table',
		targetEngineType: 'postgresql',
		targetRepresentation: 'native',
		sourceFields: [{ name: 'id', type: 'bigint', primary_key: true }]
	}), [{ code: 'capabilityUnavailable' }])
})

test('task engine descriptors restore types and capabilities from System engines', () => {
	const task = {
		config: {
			source: { locator: 'addp://engine/8/path/public/source?type=table' },
			target: { parent_locator: 'addp://engine/20/path/tiger?type=schema' }
		}
	}
	const sourceCapabilities = { storage: { store: { bounded_watermark_read: true } } }
	const targetCapabilities = { storage: { store: { table_upsert: { supported: true, idempotent: true } } } }
	assert.deepEqual(taskEngineDescriptors(task, [
		{ id: 8, engine_type: 'mysql', capabilities: sourceCapabilities },
		{ id: 20, engine_type: 'postgresql', capabilities: targetCapabilities }
	]), {
		source: { engine_type: 'mysql', capabilities: sourceCapabilities },
		target: { engine_type: 'postgresql', capabilities: targetCapabilities }
	})
	assert.deepEqual(taskEngineDescriptors(task, []), { source: null, target: null })
})
