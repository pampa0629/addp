import assert from 'node:assert/strict'
import test from 'node:test'
import { formatResultCell, resultSelectionFromRow } from '../src/utils/tabularResult.js'

test('formats scalar, null and structured result cells without field assumptions', () => {
  assert.equal(formatResultCell(null), '—')
  assert.equal(formatResultCell(false), 'false')
  assert.equal(formatResultCell(12.5), '12.5')
  assert.equal(formatResultCell({ nested: true }), '{"nested":true}')
})

test('maps a clicked table row to the original result index', () => {
  const rows = [{ id: 1 }, { id: 2 }]
  assert.deepEqual(resultSelectionFromRow(rows, rows[1]), { row_index: 1 })
  assert.equal(resultSelectionFromRow(rows, { id: 2 }), null)
})
