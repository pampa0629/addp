import test from 'node:test'
import assert from 'node:assert/strict'

import {
  applyDecimalRecommendations,
  decimalFactsFromField,
  mysqlDecimalMappingIssues,
  mysqlDecimalMappingsValid
} from '../src/views/TaskWizard/decimalMapping.mjs'

test('decimalFactsFromField keeps declared precision and scale', () => {
  assert.deepEqual(
    decimalFactsFromField({ type: 'decimal', precision: 18, scale: 4 }),
    { precision: 18, scale: 4 }
  )
  assert.deepEqual(decimalFactsFromField({ type: 'decimal' }), {})
})

test('mysqlDecimalMappingsValid requires bounded decimal facts', () => {
  const mappings = [{ source_field: 'amount', target_field: 'amount', target_type: 'decimal' }]
  const unboundedSource = [{ name: 'amount', type: 'decimal' }]

  assert.equal(mysqlDecimalMappingsValid(mappings, unboundedSource, 'mysql', 'native'), false)
  assert.equal(mysqlDecimalMappingsValid([
    { ...mappings[0], precision: 20, scale: 10 }
  ], unboundedSource, 'mysql', 'native'), true)
  assert.equal(mysqlDecimalMappingsValid(mappings, unboundedSource, 'postgresql', 'native'), true)
})

test('mysqlDecimalMappingsValid accepts inherited source precision', () => {
  assert.equal(mysqlDecimalMappingsValid([
    { source_field: 'amount', target_field: 'amount', target_type: 'decimal' }
  ], [
    { name: 'amount', type: 'decimal', precision: 18, scale: 4 }
  ], 'mysql', 'native'), true)
})

test('mysqlDecimalMappingIssues identifies the invalid mapping and reason', () => {
  const mappings = [
    { source_field: 'name', target_field: 'name', target_type: 'string' },
    { source_field: 'area', target_field: 'area', target_type: 'decimal' },
    { source_field: 'length', target_field: 'length', target_type: 'decimal', precision: 10, scale: 11 }
  ]

  assert.deepEqual(
    mysqlDecimalMappingIssues(mappings, [], [], 'mysql', 'native'),
    [
      { index: 1, sourceField: 'area', targetField: 'area', code: 'precision_required' },
      { index: 2, sourceField: 'length', targetField: 'length', code: 'scale_exceeds_precision' }
    ]
  )
})

test('mysql decimal validation inherits matching existing target field facts', () => {
  const mappings = [{ source_field: 'area', target_field: 'area', target_type: 'decimal' }]
  const sourceFields = [{ name: 'area', type: 'decimal' }]
  const targetFields = [{ name: 'area', type: 'decimal', precision: 20, scale: 6 }]

  assert.equal(mysqlDecimalMappingsValid(mappings, sourceFields, 'mysql', 'native', targetFields), true)
  assert.deepEqual(mysqlDecimalMappingIssues(mappings, sourceFields, targetFields, 'mysql', 'native'), [])
})

test('mysql decimal validation distinguishes stale existing target metadata', () => {
  const mappings = [{ source_field: 'area', target_field: 'area', target_type: 'decimal' }]
  const sourceFields = [{ name: 'area', type: 'decimal' }]
  const targetFields = [{ name: 'area', type: 'decimal', native_type: 'decimal(20,10)' }]

  assert.deepEqual(
    mysqlDecimalMappingIssues(mappings, sourceFields, targetFields, 'mysql', 'native'),
    [{ index: 0, sourceField: 'area', targetField: 'area', code: 'target_definition_missing' }]
  )
  assert.deepEqual(
    mysqlDecimalMappingIssues(
      [{ ...mappings[0], precision: 20 }],
      sourceFields,
      targetFields,
      'mysql',
      'native'
    ),
    [{ index: 0, sourceField: 'area', targetField: 'area', code: 'scale_required' }]
  )
})

test('exact source recommendations update matching decimal mappings only', () => {
  const mappings = [
    { source_field: 'amount', target_field: 'amount', target_type: 'decimal' },
    { source_field: 'name', target_field: 'name', target_type: 'string' }
  ]
  assert.deepEqual(applyDecimalRecommendations(mappings, [
    { source_field: 'amount', precision: 18, scale: 6 }
  ]), [
    { source_field: 'amount', target_field: 'amount', target_type: 'decimal', precision: 18, scale: 6 },
    mappings[1]
  ])
})
