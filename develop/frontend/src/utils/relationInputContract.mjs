const relationAliasPattern = /^[a-z][a-z0-9_]*$/

export function normalizeRelationInputs(values) {
  return [...new Set(
    (Array.isArray(values) ? values : [])
      .map(value => String(value || '').trim().toLowerCase())
      .filter(Boolean)
  )]
}

export function invalidRelationInputs(values) {
  return normalizeRelationInputs(values).filter(value => !relationAliasPattern.test(value))
}

export function relationInputsValid(values) {
  const normalized = normalizeRelationInputs(values)
  return normalized.length === (Array.isArray(values) ? values.filter(value => String(value || '').trim()).length : 0) &&
    invalidRelationInputs(normalized).length === 0
}
