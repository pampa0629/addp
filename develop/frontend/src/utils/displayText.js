function normalizeDisplayText(value) {
  return String(value ?? '').trim().toLocaleLowerCase()
}

export function hasDistinctText(value, ...existingValues) {
  const normalized = normalizeDisplayText(value)
  if (!normalized) return false
  return existingValues.every(existing => normalizeDisplayText(existing) !== normalized)
}
