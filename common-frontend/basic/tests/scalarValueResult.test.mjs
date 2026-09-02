import assert from 'node:assert/strict'
import test from 'node:test'
import { formatScalarValue, validateScalarValueResult } from '../src/utils/scalarValueResult.mjs'

const config = { items: [{ field: 'total', label: 'Total', unit: 'items', precision: 0 }] }

test('accepts one complete service row without computing a value in the renderer', () => {
  assert.deepEqual(validateScalarValueResult([{ total: 12 }], config), { valid: true, reason: '' })
  assert.equal(formatScalarValue(12.345, 2, 'en-US'), '12.35')
})

test('rejects partial, empty, multi-row, duplicated, and non-numeric value results', () => {
  assert.equal(validateScalarValueResult([{ total: 12 }], config, true).reason, 'partial_result')
  assert.equal(validateScalarValueResult([], config).reason, 'single_row_required')
  assert.equal(validateScalarValueResult([{ total: 1 }, { total: 2 }], config).reason, 'single_row_required')
  assert.equal(validateScalarValueResult([{ total: null }], config).reason, 'invalid_measure')
  assert.equal(validateScalarValueResult([{ total: 1 }], { items: [config.items[0], config.items[0]] }).reason, 'invalid_config')
})

