import test from 'node:test'
import assert from 'node:assert/strict'

import {
  applyDecimalRecommendations,
  decimalMappingIssues,
  decimalMappingsValid,
  decimalTableWriteLimits,
  decimalFactsFromField,
} from '../src/views/TaskWizard/decimalMapping.mjs'

const boundedDecimalCapabilities = {
  limits: {
    table_write: {
      decimal: {
        requires_explicit_precision_scale: true,
        max_precision: 65,
        max_scale: 30
      }
    }
  }
}

test('decimalFactsFromField keeps declared precision and scale', () => {
  assert.deepEqual(
    decimalFactsFromField({ type: 'decimal', precision: 18, scale: 4 }),
    { precision: 18, scale: 4 }
  )
  assert.deepEqual(decimalFactsFromField({ type: 'decimal' }), {})
})

test('decimalMappingsValid requires facts when target capabilities require them', () => {
  const mappings = [{ source_field: 'amount', target_field: 'amount', target_type: 'decimal' }]
  const unboundedSource = [{ name: 'amount', type: 'decimal' }]

  assert.equal(decimalMappingsValid(mappings, unboundedSource, boundedDecimalCapabilities, 'native'), false)
  assert.equal(decimalMappingsValid([
    { ...mappings[0], precision: 20, scale: 10 }
  ], unboundedSource, boundedDecimalCapabilities, 'native'), true)
  assert.equal(decimalMappingsValid(mappings, unboundedSource, {}, 'native'), true)
})

test('decimalMappingsValid accepts inherited source precision', () => {
  assert.equal(decimalMappingsValid([
    { source_field: 'amount', target_field: 'amount', target_type: 'decimal' }
  ], [
    { name: 'amount', type: 'decimal', precision: 18, scale: 4 }
  ], boundedDecimalCapabilities, 'native'), true)
})

test('decimalMappingIssues identifies the invalid mapping and reason', () => {
  const mappings = [
    { source_field: 'name', target_field: 'name', target_type: 'string' },
    { source_field: 'area', target_field: 'area', target_type: 'decimal' },
    { source_field: 'length', target_field: 'length', target_type: 'decimal', precision: 10, scale: 11 }
  ]

  assert.deepEqual(
    decimalMappingIssues(mappings, [], [], boundedDecimalCapabilities, 'native'),
    [
      { index: 1, sourceField: 'area', targetField: 'area', code: 'precision_required' },
      { index: 2, sourceField: 'length', targetField: 'length', code: 'scale_exceeds_precision' }
    ]
  )
})

test('bounded decimal validation inherits matching existing target field facts', () => {
  const mappings = [{ source_field: 'area', target_field: 'area', target_type: 'decimal' }]
  const sourceFields = [{ name: 'area', type: 'decimal' }]
  const targetFields = [{ name: 'area', type: 'decimal', precision: 20, scale: 6 }]

  assert.equal(decimalMappingsValid(mappings, sourceFields, boundedDecimalCapabilities, 'native', targetFields), true)
  assert.deepEqual(decimalMappingIssues(mappings, sourceFields, targetFields, boundedDecimalCapabilities, 'native'), [])
})

test('bounded decimal validation distinguishes stale existing target metadata', () => {
  const mappings = [{ source_field: 'area', target_field: 'area', target_type: 'decimal' }]
  const sourceFields = [{ name: 'area', type: 'decimal' }]
  const targetFields = [{ name: 'area', type: 'decimal', native_type: 'decimal(20,10)' }]

  assert.deepEqual(
    decimalMappingIssues(mappings, sourceFields, targetFields, boundedDecimalCapabilities, 'native'),
    [{ index: 0, sourceField: 'area', targetField: 'area', code: 'target_definition_missing' }]
  )
  assert.deepEqual(
    decimalMappingIssues(
      [{ ...mappings[0], precision: 20 }],
      sourceFields,
      targetFields,
      boundedDecimalCapabilities,
      'native'
    ),
    [{ index: 0, sourceField: 'area', targetField: 'area', code: 'scale_required' }]
  )
})

test('decimal validation is capability-driven rather than engine-name-driven', () => {
  const mappings = [{ source_field: 'amount', target_field: 'amount', target_type: 'decimal' }]
  const sourceFields = [{ name: 'amount', type: 'decimal' }]

  assert.equal(decimalMappingsValid(mappings, sourceFields, boundedDecimalCapabilities, 'native'), false)
  assert.equal(decimalMappingsValid(mappings, sourceFields, {}, 'native'), true)
  assert.deepEqual(decimalTableWriteLimits(boundedDecimalCapabilities), {
    requiresExplicitPrecisionScale: true,
    maxPrecision: 65,
    maxScale: 30
  })
  assert.equal(decimalTableWriteLimits({ limits: { table_write: { decimal: {} } } }), null)
})

test('decimal validation consumes target-specific maximums', () => {
  const capabilities = {
    limits: { table_write: { decimal: { max_precision: 10, max_scale: 2 } } }
  }
  const mappings = [
    { source_field: 'amount', target_field: 'amount', target_type: 'decimal', precision: 11, scale: 2 },
    { source_field: 'tax', target_field: 'tax', target_type: 'decimal', precision: 10, scale: 3 }
  ]
  assert.deepEqual(decimalMappingIssues(mappings, [], [], capabilities, 'native'), [
    { index: 0, sourceField: 'amount', targetField: 'amount', code: 'precision_exceeds_max' },
    { index: 1, sourceField: 'tax', targetField: 'tax', code: 'scale_exceeds_max' }
  ])
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
