import assert from 'node:assert/strict'
import test from 'node:test'
import {
  formatResultCell,
  lastPage,
  normalizeTabularColumns,
  paginateRows,
  resultSelectionFromRow,
  safeStructuredResultJSON,
  tabularCellValue
} from '../src/utils/tabularResult.js'

test('formats scalar, null and structured result cells without field assumptions', () => {
  assert.equal(formatResultCell(null), '—')
  assert.equal(formatResultCell(false), 'false')
  assert.equal(formatResultCell(12.5), '12.5')
  assert.equal(formatResultCell({ nested: true }), '{"nested":true}')
  assert.equal(formatResultCell(null, 'NULL'), 'NULL')
})

test('normalizes string and path-based columns through one table descriptor contract', () => {
  const rows = [{ id: 1, profile: { name: 'Ada' } }]
  const columns = normalizeTabularColumns({
    columns: ['id', { key: 'profile.name', label: 'Name', path: ['profile', 'name'] }],
    rows
  })

  assert.deepEqual(columns.map(column => column.key), ['id', 'profile.name'])
  assert.equal(tabularCellValue(rows[0], columns[1]), 'Ada')
})

test('applies published field labels and widths while preserving source column keys', () => {
  const columns = normalizeTabularColumns({
    columns: ['amount'],
    fields: [{ name: 'amount', comment: 'Raw amount' }],
    presentations: [{ field: 'amount', label: '订单金额', unit: '元', precision: 2, width: 180 }],
  })

  assert.deepEqual(columns[0], {
    key: 'amount', label: '订单金额', path: ['amount'], width: 180,
    presentation: { field: 'amount', label: '订单金额', unit: '元', precision: 2, width: 180 },
  })
  assert.equal(formatResultCell(12.5, '—', columns[0].presentation, 'zh-CN'), '12.50 元')
})

test('paginates an already loaded bounded result without changing the source rows', () => {
  const rows = Array.from({ length: 25 }, (_, index) => ({ id: index + 1 }))

  assert.deepEqual(paginateRows(rows, 2, 10).map(row => row.id), [11, 12, 13, 14, 15, 16, 17, 18, 19, 20])
  assert.equal(lastPage(rows.length, 10), 3)
  assert.equal(rows.length, 25)
})

test('formats structured values safely for the shared detail dialog', () => {
  assert.equal(safeStructuredResultJSON({ nested: true }), '{\n  "nested": true\n}')
  const circular = {}
  circular.self = circular
  assert.equal(safeStructuredResultJSON(circular), '[object]')
})

test('maps a clicked table row to the original result index', () => {
  const rows = [{ id: 1 }, { id: 2 }]
  assert.deepEqual(resultSelectionFromRow(rows, rows[1]), { row_index: 1 })
  assert.equal(resultSelectionFromRow(rows, { id: 2 }), null)
})
