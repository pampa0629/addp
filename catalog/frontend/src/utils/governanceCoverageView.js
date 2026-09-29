const dimensionFields = new Set(['name', 'description'])
const coverageDimensions = new Set([
  'business_definition', 'primary_domain', 'accountable_department', 'business_owner',
  'data_steward', 'glossary', 'component_standard_mapping'
])
const curationCoverageDimensions = new Set([
  'business_definition', 'business_owner', 'data_steward', 'glossary', 'component_standard_mapping'
])
const governanceStatuses = new Set(['discovered', 'curated', 'certified', 'deprecated'])

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

export function coverageGapDetailTab(dimensionKey, state) {
  return state === 'missing' && curationCoverageDimensions.has(dimensionKey) ? 'curation' : ''
}

export function buildGovernanceStatusEntryQuery(status) {
  if (!governanceStatuses.has(status)) return null
  return { view: 'inventory', governance_status: status }
}
