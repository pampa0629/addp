export function catalogStatusLabel(translate, keyPrefix, value, emptyLabel = '-') {
  if (typeof value !== 'string' || value.length === 0) return emptyLabel
  return translate(`${keyPrefix}.${value}`)
}
