const DEFAULT_PAGE_SIZE = 20
const ALLOWED_PAGE_SIZES = new Set([20, 50, 100])

function queryValue(value) {
  const firstValue = Array.isArray(value) ? value[0] : value
  return firstValue == null ? '' : String(firstValue).trim()
}

function positiveInteger(value, fallback) {
  const normalized = queryValue(value)
  if (!/^\d+$/.test(normalized)) return fallback

  const number = Number(normalized)
  return Number.isSafeInteger(number) && number > 0 ? number : fallback
}

function queriesEqual(left, right) {
  const rightKeys = Object.keys(right)
  return Object.keys(left).length === rightKeys.length && rightKeys.every(key => (
    !Array.isArray(left[key]) && String(left[key]) === right[key]
  ))
}

export function buildRuleApplicationListRouteQuery({ engineID, schemaName, tableName, page, pageSize }) {
  const query = {}
  if (engineID) query.engine_id = String(engineID)
  if (schemaName) query.schema_name = schemaName
  if (tableName) query.table_name = tableName
  if (page > 1) query.page = String(page)
  if (pageSize !== DEFAULT_PAGE_SIZE) query.page_size = String(pageSize)
  return query
}

export function resolveRuleApplicationListRouteState(routeQuery = {}) {
  const engineID = positiveInteger(routeQuery.engine_id, null)
  const schemaName = queryValue(routeQuery.schema_name)
  const tableName = queryValue(routeQuery.table_name)
  const page = positiveInteger(routeQuery.page, 1)
  const requestedPageSize = positiveInteger(routeQuery.page_size, DEFAULT_PAGE_SIZE)
  const pageSize = ALLOWED_PAGE_SIZES.has(requestedPageSize) ? requestedPageSize : DEFAULT_PAGE_SIZE
  const query = buildRuleApplicationListRouteQuery({ engineID, schemaName, tableName, page, pageSize })

  return {
    engineID,
    schemaName,
    tableName,
    page,
    pageSize,
    query,
    changed: !queriesEqual(routeQuery, query)
  }
}
