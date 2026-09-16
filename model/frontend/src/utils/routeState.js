const DEFAULT_PAGE_SIZE = 20
const ALLOWED_PAGE_SIZES = new Set([20, 50, 100])
const ALLOWED_STATUSES = new Set(['draft', 'approved'])

const queryValue = value => {
  const firstValue = Array.isArray(value) ? value[0] : value
  return firstValue == null ? '' : String(firstValue).trim()
}

const positiveInteger = (value, fallback) => {
  const parsed = Number(queryValue(value))
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback
}

const queriesEqual = (left, right) => {
  const rightKeys = Object.keys(right)
  return Object.keys(left).length === rightKeys.length &&
    rightKeys.every(key => !Array.isArray(left[key]) && String(left[key] ?? '') === right[key])
}

const buildListRouteQuery = ({ keyword, domainId, status, page, pageSize, layer }) => {
  const query = {}
  const normalizedKeyword = String(keyword || '').trim()
  const normalizedLayer = String(layer || '').trim()

  if (normalizedKeyword) query.keyword = normalizedKeyword
  if (domainId) query.domain_id = String(domainId)
  if (normalizedLayer) query.layer = normalizedLayer
  if (ALLOWED_STATUSES.has(status)) query.status = status
  if (page > 1) query.page = String(page)
  if (pageSize !== DEFAULT_PAGE_SIZE) query.page_size = String(pageSize)

  return query
}

const resolveListRouteState = (routeQuery, { includeLayer }) => {
  const keyword = queryValue(routeQuery.keyword)
  const domainId = positiveInteger(routeQuery.domain_id, null)
  const statusValue = queryValue(routeQuery.status)
  const status = ALLOWED_STATUSES.has(statusValue) ? statusValue : ''
  const page = positiveInteger(routeQuery.page, 1)
  const requestedPageSize = positiveInteger(routeQuery.page_size, DEFAULT_PAGE_SIZE)
  const pageSize = ALLOWED_PAGE_SIZES.has(requestedPageSize) ? requestedPageSize : DEFAULT_PAGE_SIZE
  const layer = includeLayer ? queryValue(routeQuery.layer) : ''
  const query = buildListRouteQuery({ keyword, domainId, status, page, pageSize, layer })

  return {
    keyword,
    domainId,
    status,
    page,
    pageSize,
    layer,
    query,
    changed: !queriesEqual(routeQuery, query)
  }
}

export const buildEntityListRouteQuery = state => buildListRouteQuery({ ...state, layer: '' })

export const resolveEntityListRouteState = (routeQuery = {}) =>
  resolveListRouteState(routeQuery, { includeLayer: false })

export const buildLogicalTableListRouteQuery = state => buildListRouteQuery(state)

export const resolveLogicalTableListRouteState = (routeQuery = {}) =>
  resolveListRouteState(routeQuery, { includeLayer: true })

export const buildERDiagramRouteQuery = ({ domainId, includeRelated = false }) => {
  const query = domainId ? { domain_id: String(domainId) } : {}
  if (domainId && domainId !== 'all' && includeRelated) query.related = '1'
  return query
}

export const resolveERDiagramRouteState = (routeQuery = {}) => {
  const domainId = routeQuery.domain_id === 'all' ? 'all' : positiveInteger(routeQuery.domain_id, null)
  const includeRelated = Boolean(domainId && domainId !== 'all' && routeQuery.related === '1')
  const query = buildERDiagramRouteQuery({ domainId, includeRelated })
  return { domainId, includeRelated, query, changed: !queriesEqual(routeQuery, query) }
}

export const buildDimensionalModelRouteQuery = ({ domainId, tableId }) => {
  const query = {}
  if (domainId) query.domain_id = String(domainId)
  if (tableId) query.table_id = String(tableId)
  return query
}

export const resolveDimensionalModelRouteState = (routeQuery = {}) => {
  const domainId = positiveInteger(routeQuery.domain_id, null)
  const tableId = positiveInteger(routeQuery.table_id, null)
  const query = buildDimensionalModelRouteQuery({ domainId, tableId })
  return { domainId, tableId, query, changed: !queriesEqual(routeQuery, query) }
}

export const resolveLogicalTableDetailRouteState = (routeQuery = {}, tableType) => {
  const query = resolveLogicalTableListRouteState(routeQuery).query
  const supportsRelations = tableType === 'fact' || tableType === 'dimension'
  const requestedTab = queryValue(routeQuery.tab)
  const tab = requestedTab === 'physical-target'
    ? 'physical-target'
    : requestedTab === 'concept-mappings'
    ? 'concept-mappings'
    : supportsRelations && requestedTab === 'relations' ? 'relations' : 'definition'
  const relationId = tab === 'relations' ? positiveInteger(routeQuery.relation_id, null) : null
  if (tab !== 'definition') query.tab = tab
  if (relationId) query.relation_id = String(relationId)
  return { tab, relationId, query, changed: !queriesEqual(routeQuery, query) }
}

export const buildTableRelationRoute = (tableId, relationId = null, contextQuery = {}) => ({
  path: `/logical-tables/${tableId}`,
  query: resolveLogicalTableDetailRouteState({ ...contextQuery, tab: 'relations', relation_id: relationId }, 'fact').query
})
