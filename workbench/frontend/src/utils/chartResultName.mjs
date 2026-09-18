// A chart's displayed name describes its complete result, not a cached directory row.
export function chartResultName(rows, field, ready, hasMore) {
  if (!field || !ready || !rows.length) return null
  if (hasMore) return { status: 'ambiguous' }
  const values = rows.map(row => row?.[field])
  if (values.every(value => value == null || (typeof value === 'string' && !value.trim()))) return { status: 'missing' }
  if (values.some(value => typeof value !== 'string' || !value.trim()) || new Set(values).size !== 1) return { status: 'ambiguous' }
  return { status: 'ready', value: values[0] }
}
