export function formatResultCell(value) {
  if (value === null || value === undefined) return '—'
  if (typeof value === 'boolean') return value ? 'true' : 'false'
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

export function resultSelectionFromRow(rows, row) {
  const rowIndex = Array.isArray(rows) ? rows.indexOf(row) : -1
  return rowIndex >= 0 ? { row_index: rowIndex } : null
}
