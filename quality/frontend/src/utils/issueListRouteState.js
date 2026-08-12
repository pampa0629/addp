const DEFAULT_PAGE_SIZE = 20
const ALLOWED_PAGE_SIZES = new Set([20, 50, 100])
const ALLOWED_STATUSES = new Set(['open', 'resolved', 'ignored'])

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
    !Array.isArray(left[key]) && queryValue(left[key]) === right[key]
  ))
}

export function buildIssueListRouteQuery({ status, engineID, page, pageSize }) {
  const query = {}
  if (ALLOWED_STATUSES.has(status)) query.status = status
  if (engineID) query.engine_id = String(engineID)
  if (page > 1) query.page = String(page)
  if (pageSize !== DEFAULT_PAGE_SIZE) query.page_size = String(pageSize)
  return query
}

export function resolveIssueListRouteState(routeQuery = {}) {
  const statusValue = queryValue(routeQuery.status)
  const status = ALLOWED_STATUSES.has(statusValue) ? statusValue : ''
  const engineID = positiveInteger(routeQuery.engine_id, null)
  const page = positiveInteger(routeQuery.page, 1)
  const requestedPageSize = positiveInteger(routeQuery.page_size, DEFAULT_PAGE_SIZE)
  const pageSize = ALLOWED_PAGE_SIZES.has(requestedPageSize) ? requestedPageSize : DEFAULT_PAGE_SIZE
  const query = buildIssueListRouteQuery({ status, engineID, page, pageSize })

  return {
    status,
    engineID,
    page,
    pageSize,
    query,
    changed: !queriesEqual(routeQuery, query)
  }
}
