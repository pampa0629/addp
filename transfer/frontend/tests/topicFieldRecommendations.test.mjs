import assert from 'node:assert/strict'
import test from 'node:test'

import {
  inferTopicFieldRecommendations,
  mergeTopicFieldRecommendations
} from '../src/views/TaskWizard/topicFieldRecommendations.mjs'

test('topic samples infer the union of top-level fields and nullable coverage', () => {
  const recommendations = inferTopicFieldRecommendations([
    { value: { id: 1, name: 'first', active: true } },
    { value: { id: 2, name: null, score: 9.5 } },
    { value: 'not-an-object' }
  ])

  assert.deepEqual(recommendations, [
    { name: 'id', type: 'bigint', nullable: false, present_count: 2, sample_count: 2 },
    { name: 'name', type: 'string', nullable: true, present_count: 2, sample_count: 2 },
    { name: 'active', type: 'bool', nullable: true, present_count: 1, sample_count: 2 },
    { name: 'score', type: 'double', nullable: true, present_count: 1, sample_count: 2 }
  ])
})

test('topic samples promote numeric types and use json for structured or incompatible values', () => {
  const recommendations = inferTopicFieldRecommendations([
    { value: { amount: 1, payload: { nested: true }, mixed: 'one', unknown: null } },
    { value: { amount: 1.25, payload: [1, 2], mixed: 2, unknown: null } }
  ])

  assert.deepEqual(recommendations, [
    { name: 'amount', type: 'double', nullable: false, present_count: 2, sample_count: 2 },
    { name: 'payload', type: 'json', nullable: false, present_count: 2, sample_count: 2 },
    { name: 'mixed', type: 'json', nullable: false, present_count: 2, sample_count: 2 },
    { name: 'unknown', type: 'json', nullable: true, present_count: 2, sample_count: 2 }
  ])
})

test('confirmed recommendations preserve manual mappings and fields absent from samples', () => {
  const existingMappings = [
    { source_field: 'id', target_field: 'order_id', target_type: 'string', nullable: false },
    { source_field: 'legacy', target_field: 'legacy_value', target_type: 'string', nullable: true },
    { source_field: '', target_field: '', target_type: 'string', nullable: true }
  ]
  const existingSourceFields = [
    { name: 'id', type: 'string', nullable: false },
    { name: 'legacy', type: 'string', nullable: true }
  ]

  const result = mergeTopicFieldRecommendations({
    recommendations: [
      { name: 'id', target_name: 'id', type: 'bigint', nullable: false },
      { name: 'status', target_name: 'order_status', type: 'string', nullable: true }
    ],
    sourceFields: existingSourceFields,
    fieldMappings: existingMappings,
    targetFields: [
      { name: 'order_status', type: 'string', nullable: false }
    ]
  })

  assert.equal(result.addedCount, 1)
  assert.deepEqual(result.sourceFields, [
    { name: 'id', type: 'string', nullable: false },
    { name: 'legacy', type: 'string', nullable: true },
    { name: 'status', type: 'string', nullable: true }
  ])
  assert.deepEqual(result.fieldMappings, [
    { source_field: 'id', target_field: 'order_id', target_type: 'string', nullable: false },
    { source_field: 'legacy', target_field: 'legacy_value', target_type: 'string', nullable: true },
    {
      source_field: 'status',
      target_field: 'order_status',
      target_type: 'string',
      format: '',
      default_value: '',
      nullable: false
    }
  ])
})

