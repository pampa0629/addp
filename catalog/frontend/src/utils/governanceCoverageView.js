const dimensionFields = new Set(['name', 'description'])
const coverageDimensions = new Set([
  'business_definition', 'primary_domain', 'accountable_department', 'business_owner',
  'data_steward', 'glossary', 'component_element'
])

export function coverageDimensionLabel(translate, dimensionKey, field, emptyLabel = '-') {
  if (typeof dimensionKey !== 'string' || dimensionKey.length === 0 || !dimensionFields.has(field)) {
    return emptyLabel
  }
  return translate(`catalog.coverage.dimensions.${dimensionKey}.${field}`)
}

export function buildMissingCoverageEntryQuery(dimensionKey) {
  if (!coverageDimensions.has(dimensionKey)) return null
  return { view: 'inventory', coverage_dimension: dimensionKey, coverage_state: 'missing' }
}
