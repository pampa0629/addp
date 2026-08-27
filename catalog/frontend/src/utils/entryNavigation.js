import { parseEntryListRoute } from './entryRouteState'

export function buildEntryFacetQuery(state) {
  const parsed = parseEntryListRoute(state)
  const query = { view: parsed.view }
  if (parsed.primary_domain_id) query.primary_domain_id = parsed.primary_domain_id
  if (parsed.accountable_department_id) query.accountable_department_id = parsed.accountable_department_id
  if (parsed.entry_type) query.entry_type = parsed.entry_type
  return query
}

export function applyEntryNavigationSelection(state, dimension, value) {
  const next = {
    ...parseEntryListRoute(state),
    coverage_dimension: '',
    coverage_state: '',
    page: 1
  }
  if (dimension === 'primary_domain') {
    next.primary_domain_id = value
    next.accountable_department_id = ''
    next.entry_type = ''
  } else if (dimension === 'accountable_department') {
    next.accountable_department_id = value
    next.entry_type = ''
  } else if (dimension === 'entry_type') {
    next.entry_type = value
  }
  return parseEntryListRoute(next)
}

export function applyUnclassifiedDomainSelection(state) {
  return parseEntryListRoute({
    ...state,
    view: 'inventory',
    search: '',
    primary_domain_id: '',
    accountable_department_id: '',
    entry_type: '',
    coverage_dimension: 'primary_domain',
    coverage_state: 'missing',
    page: 1
  })
}

export function applyUnassignedDepartmentSelection(state) {
  return parseEntryListRoute({
    ...state,
    view: 'inventory',
    search: '',
    accountable_department_id: '',
    entry_type: '',
    coverage_dimension: 'accountable_department',
    coverage_state: 'missing',
    page: 1
  })
}
