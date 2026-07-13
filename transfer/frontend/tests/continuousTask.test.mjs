import test from 'node:test'
import assert from 'node:assert/strict'

import {
  continuousMappedTargetKeys,
  continuousMappingsValid,
  isKafkaTopicSource
} from '../src/views/TaskWizard/continuousTask.mjs'

test('Kafka topic source is the only continuous source shape', () => {
  assert.equal(isKafkaTopicSource('kafka', 'addp://engine/30/path/orders.events?type=topic'), true)
  assert.equal(isKafkaTopicSource('postgresql', 'addp://engine/30/path/orders.events?type=topic'), false)
  assert.equal(isKafkaTopicSource('kafka', 'addp://engine/30/path/orders?type=table'), false)
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
