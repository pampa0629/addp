import test from 'node:test'
import assert from 'node:assert/strict'

import {
  decimalFactsFromSourceField,
  mysqlDecimalMappingsValid
} from '../src/views/TaskWizard/decimalMapping.mjs'

test('decimalFactsFromSourceField keeps declared precision and scale', () => {
  assert.deepEqual(
    decimalFactsFromSourceField({ type: 'decimal', precision: 18, scale: 4 }),
    { precision: 18, scale: 4 }
  )
  assert.deepEqual(decimalFactsFromSourceField({ type: 'decimal' }), {})
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
