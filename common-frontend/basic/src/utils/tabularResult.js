export function formatResultCell(value, nullText = '—') {
  if (value === null || value === undefined) return nullText
  if (typeof value === 'boolean') return value ? 'true' : 'false'
  if (typeof value === 'object') {
    try {
      return JSON.stringify(value)
    } catch {
      return '[object]'
    }
  }
  return String(value)
}

export function normalizeTabularColumns({ columns = [], fields = [], rows = [] } = {}) {
  const fieldLabels = new Map(
    (Array.isArray(fields) ? fields : [])
      .filter(field => field?.name)
      .map(field => [field.name, field.comment || field.name])
  )
  const source = Array.isArray(columns) && columns.length > 0
    ? columns
    : (Array.isArray(fields) && fields.length > 0
        ? fields.map(field => field.name)
        : Object.keys(rows?.[0] || {}))

  return source
    .map((column) => {
      if (typeof column === 'string') {
        return {
          key: column,
          label: fieldLabels.get(column) || column,
          path: [column]
        }
      }
      if (!column || typeof column !== 'object') return null
      const key = String(column.key || column.name || column.prop || '').trim()
      if (!key) return null
      return {
        ...column,
        key,
        label: column.label || fieldLabels.get(key) || key,
        path: Array.isArray(column.path) && column.path.length > 0 ? column.path : [key]
      }
    })
    .filter(Boolean)
}

export function tabularCellValue(row, column) {
  if (typeof column?.value === 'function') return column.value(row)
  const path = Array.isArray(column?.path) && column.path.length > 0
    ? column.path
    : [column?.key]
  return path.reduce((value, segment) => value?.[segment], row)
}

export function isStructuredResultValue(value) {
  return value !== null && typeof value === 'object'
}

export function safeStructuredResultJSON(value) {
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return formatResultCell(value)
  }
}

export function paginateRows(rows, currentPage, pageSize) {
  if (!Array.isArray(rows) || rows.length === 0) return []
  const normalizedSize = Math.max(1, Number(pageSize) || 1)
  const normalizedPage = Math.max(1, Number(currentPage) || 1)
  const start = (normalizedPage - 1) * normalizedSize
  return rows.slice(start, start + normalizedSize)
}

export function lastPage(total, pageSize) {
  const normalizedTotal = Math.max(0, Number(total) || 0)
  const normalizedSize = Math.max(1, Number(pageSize) || 1)
  return Math.max(1, Math.ceil(normalizedTotal / normalizedSize))
}

export function resultSelectionFromRow(rows, row) {
  const rowIndex = Array.isArray(rows) ? rows.indexOf(row) : -1
  return rowIndex >= 0 ? { row_index: rowIndex } : null
}
