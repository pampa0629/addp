const ALLOWED_SOURCE_STATUSES = new Set(['active', 'missing'])
const ALLOWED_ENTRY_TYPES = new Set(['data_item', 'business_entity', 'logical_model', 'metric', 'data_service', 'development_artifact', 'data_application'])
const ALLOWED_VIEWS = new Set(['governance', 'inventory'])
const ALLOWED_GOVERNANCE_STATUSES = new Set(['discovered', 'curated', 'certified', 'deprecated'])
const ALLOWED_VISIBILITIES = new Set(['inventory', 'department', 'tenant'])
const ALLOWED_COVERAGE_DIMENSIONS = new Set([
  'business_definition', 'primary_domain', 'accountable_department', 'business_owner',
  'data_steward', 'glossary', 'component_element'
])
const MAX_INT64 = 9223372036854775807n

function positiveInteger(value, fallback) {
  const parsed = Number.parseInt(String(value ?? ''), 10)
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback
}

function allowedValue(value, allowed) {
  const normalized = typeof value === 'string' ? value.trim() : ''
  return allowed.has(normalized) ? normalized : ''
}

function canonicalPositiveID(value) {
  const normalized = typeof value === 'string' ? value.trim() : String(value ?? '')
  if (!/^[1-9]\d*$/.test(normalized)) return ''
  try {
    return BigInt(normalized) <= MAX_INT64 ? normalized : ''
  } catch {
    return ''
  }
}

export function parseEntryListRoute(query = {}) {
  const view = allowedValue(query.view, ALLOWED_VIEWS) || 'governance'
  const coverageDimension = allowedValue(query.coverage_dimension, ALLOWED_COVERAGE_DIMENSIONS)
  const coverageState = query.coverage_state === 'missing' ? 'missing' : ''
  const hasCoverageGap = view === 'inventory' && coverageDimension && coverageState
  return {
    view,
    search: hasCoverageGap ? '' : (typeof query.search === 'string' ? query.search.trim() : ''),
    entry_type: allowedValue(query.entry_type, ALLOWED_ENTRY_TYPES),
    source_status: allowedValue(query.source_status, ALLOWED_SOURCE_STATUSES),
    governance_status: allowedValue(query.governance_status, ALLOWED_GOVERNANCE_STATUSES),
    visibility: allowedValue(query.visibility, ALLOWED_VISIBILITIES),
    primary_domain_id: canonicalPositiveID(query.primary_domain_id),
    accountable_department_id: canonicalPositiveID(query.accountable_department_id),
    source_engine_id: canonicalPositiveID(query.source_engine_id),
    coverage_dimension: hasCoverageGap ? coverageDimension : '',
    coverage_state: hasCoverageGap ? coverageState : '',
    page: positiveInteger(query.page, 1),
    page_size: Math.min(positiveInteger(query.page_size, 20), 200)
  }
}

export function buildEntryListQuery(state) {
  const query = {}
  const parsed = parseEntryListRoute(state)
  if (parsed.view !== 'governance') query.view = parsed.view
  if (parsed.search) query.search = parsed.search
  if (parsed.entry_type) query.entry_type = parsed.entry_type
  if (parsed.source_status) query.source_status = parsed.source_status
  if (parsed.governance_status) query.governance_status = parsed.governance_status
  if (parsed.visibility) query.visibility = parsed.visibility
  if (parsed.primary_domain_id) query.primary_domain_id = parsed.primary_domain_id
  if (parsed.accountable_department_id) query.accountable_department_id = parsed.accountable_department_id
  if (parsed.source_engine_id) query.source_engine_id = parsed.source_engine_id
  if (parsed.coverage_dimension) query.coverage_dimension = parsed.coverage_dimension
  if (parsed.coverage_state) query.coverage_state = parsed.coverage_state
  if (parsed.page !== 1) query.page = String(parsed.page)
  if (parsed.page_size !== 20) query.page_size = String(parsed.page_size)
  return query
}

export function isCanonicalEntryListQuery(query, canonical) {
  const currentKeys = Object.keys(query).sort()
  const canonicalKeys = Object.keys(canonical).sort()
  return currentKeys.length === canonicalKeys.length && currentKeys.every((key, index) => (
    key === canonicalKeys[index] && String(query[key]) === String(canonical[key])
  ))
}
