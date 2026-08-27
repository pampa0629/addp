import assert from 'node:assert/strict'
import test from 'node:test'
import { formatResultCell } from '../src/utils/tabularResult.js'

test('formats scalar, null and structured result cells without field assumptions', () => {
  assert.equal(formatResultCell(null), '—')
  assert.equal(formatResultCell(false), 'false')
  assert.equal(formatResultCell(12.5), '12.5')
  assert.equal(formatResultCell({ nested: true }), '{"nested":true}')
})
