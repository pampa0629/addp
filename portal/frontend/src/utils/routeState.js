export function normalizePositiveInteger(value, fallback) {
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback
}

function queriesEqual(left, right) {
  const rightKeys = Object.keys(right)
  return Object.keys(left).length === rightKeys.length &&
    rightKeys.every(key => String(left[key] || '') === String(right[key] || ''))
}

export function buildSearchRouteQuery({ keyword, typeId, page }) {
  const query = {}
  const normalizedKeyword = String(keyword || '').trim()
  if (normalizedKeyword) query.keyword = normalizedKeyword
  if (typeId) query.type_id = String(typeId)
  if (page > 1) query.page = String(page)
  return query
}

export function resolveSearchRouteState(routeQuery = {}) {
  const keyword = String(routeQuery.keyword || '').trim()
  const typeId = normalizePositiveInteger(routeQuery.type_id, 0)
  const page = normalizePositiveInteger(routeQuery.page, 1)
  const query = buildSearchRouteQuery({ keyword, typeId, page })
  return {
    keyword,
    typeId,
    page,
    query,
    changed: !queriesEqual(routeQuery, query)
  }
}

export function resolveCategoryRouteState(routeQuery = {}) {
  const page = normalizePositiveInteger(routeQuery.page, 1)
  const query = page > 1 ? { page: String(page) } : {}
  return {
    page,
    query,
    changed: !queriesEqual(routeQuery, query)
  }
}

export function assetDetailReturnTarget(previousRoute, categoryId) {
  if (typeof previousRoute === 'string' && previousRoute.startsWith('/portal/')) {
    return { history: 'back' }
  }

  const normalizedCategoryId = Number(categoryId)
  return Number.isInteger(normalizedCategoryId) && normalizedCategoryId > 0
    ? {
        history: 'replace',
        location: { name: 'Category', params: { id: String(normalizedCategoryId) } }
      }
    : { history: 'replace', location: { name: 'Search' } }
}
