const NUMERIC = new Set(['int', 'bigint', 'float', 'double', 'decimal'])

export function parameterControlType(type) {
  if (type === 'bool') return 'select'
  if (type === 'date') return 'date'
  if (type === 'timestamp') return 'datetime'
  return NUMERIC.has(type) ? 'number' : 'text'
}

export function parameterOptionsAllow(options, value) {
  return !options?.length || options.some((option) => option.value === value)
}

export function validParameterOptions(options, type) {
  if (!Array.isArray(options) || options.length > 100) return false
  const seen = new Set()
  return options.every(({ value, labels }) => {
    const key = JSON.stringify(value)
    const valid = type === 'bool' ? typeof value === 'boolean'
      : NUMERIC.has(type) ? typeof value === 'number' && Number.isFinite(value) && (!['int', 'bigint'].includes(type) || Number.isInteger(value))
        : typeof value === 'string' && value !== ''
    if (!valid || seen.has(key) || !labels || Object.keys(labels).length !== 2) return false
    seen.add(key)
    return ['zh-cn', 'en'].every((lang) => typeof labels[lang] === 'string' && labels[lang].trim() && [...labels[lang]].length <= 100)
  })
}

export function intersectParameterOptions(left = [], right = []) {
  if (!left.length) return right
  if (!right.length) return left
  const common = left.filter((a) => right.some((b) => a.value === b.value))
  if (!common.length) throw new Error('empty-options')
  for (const option of common) {
    const other = right.find((item) => item.value === option.value)
    if (['zh-cn', 'en'].some((lang) => option.labels[lang] !== other.labels[lang])) throw new Error('option-label-conflict')
  }
  return common
}
