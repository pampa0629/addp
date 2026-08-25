export function stringifyMetricDerivationConfig(value) {
  if (!value || typeof value !== 'object' || Array.isArray(value) || Object.keys(value).length === 0) {
    return ''
  }
  return JSON.stringify(value, null, 2)
}

export function parseMetricDerivationConfig(value) {
  const text = String(value || '').trim()
  if (!text) return null
  const parsed = JSON.parse(text)
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('metric derivation config must be a JSON object')
  }
  return parsed
}
