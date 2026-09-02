export function formatPhysicalType(field) {
  if (!field || typeof field !== 'object') return '-'
  const base = String(field.native_type || field.type || '').trim() || '-'
  const precision = Number(field.precision || 0)
  const scale = Number(field.scale || 0)
  const size = Number(field.size || 0)
  if (precision > 0) return `${base}(${precision}${scale > 0 ? `,${scale}` : ''})`
  if (size > 0) return `${base}(${size})`
  return base
}

export function formatRangeConstraint(range) {
  if (!range || typeof range !== 'object') return ''
  const left = range.min_inclusive === false ? '(' : '['
  const right = range.max_inclusive === false ? ')' : ']'
  const min = range.min == null ? '-∞' : String(range.min)
  const max = range.max == null ? '+∞' : String(range.max)
  return `${left}${min}, ${max}${right}`
}

export function activeCodeItemLabels(standard) {
  const items = standard?.code_set_revision?.items
  if (!Array.isArray(items)) return []
  return items
    .filter(item => item?.status === 'active')
    .map(item => `${String(item.code || '').trim()} · ${String(item.label || '').trim()}`)
}
