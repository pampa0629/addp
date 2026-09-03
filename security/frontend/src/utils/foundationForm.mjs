export function initialFoundationFieldValue(field, values = {}) {
  if (values[field.key] !== undefined && values[field.key] !== null) return values[field.key]
  if (field.nullable) return null
  if (field.type === 'boolean') return true
  if (field.type === 'number') return 0
  return ''
}

export function buildFoundationPayload(fields, form) {
  const payload = {}
  for (const field of fields) {
    const value = form[field.key]
    if (field.nullable && (value === null || value === undefined || value === '')) continue
    payload[field.key] = value
  }
  if (form.version !== undefined) payload.version = Number(form.version)
  return payload
}

export function sortFoundationRows(resource, rows) {
  const orderKey = resource === 'classification'
    ? 'sort_order'
    : resource === 'grade'
      ? 'risk_order'
      : null
  if (!orderKey) return [...rows]
  return [...rows].sort((left, right) => {
    const order = Number(left?.[orderKey] || 0) - Number(right?.[orderKey] || 0)
    if (order !== 0) return order
    const code = String(left?.code || '').localeCompare(String(right?.code || ''))
    if (code !== 0) return code
    return Number(left?.id || 0) - Number(right?.id || 0)
  })
}

const protectionEffects = new Set(['mask', 'suppress', 'deny'])

export function protectionEffectI18nKey(effect) {
  return protectionEffects.has(effect) ? `security.options.effects.${effect}` : null
}
