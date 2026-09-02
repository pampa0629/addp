const MAX_ITEMS = 4

export function validateScalarValueResult(rows, config, hasMore = false) {
  if (hasMore) return { valid: false, reason: 'partial_result' }
  if (!Array.isArray(rows) || !config || !Array.isArray(config.items) || config.items.length === 0 || config.items.length > MAX_ITEMS) {
    return { valid: false, reason: 'invalid_config' }
  }
  const fields = new Set()
  for (const item of config.items) {
    if (!item?.field || fields.has(item.field) || !Number.isInteger(item.precision) || item.precision < 0 || item.precision > 8) {
      return { valid: false, reason: 'invalid_config' }
    }
    fields.add(item.field)
  }
  if (rows.length !== 1) return { valid: false, reason: 'single_row_required' }
  for (const item of config.items) {
    const value = rows[0]?.[item.field]
    if (value === null || value === undefined || value === '' || !Number.isFinite(Number(value))) {
      return { valid: false, reason: 'invalid_measure' }
    }
  }
  return { valid: true, reason: '' }
}

export function formatScalarValue(value, precision, locale = 'zh-CN') {
  return new Intl.NumberFormat(locale, {
    minimumFractionDigits: precision,
    maximumFractionDigits: precision,
  }).format(Number(value))
}

