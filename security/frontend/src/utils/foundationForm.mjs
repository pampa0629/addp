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
