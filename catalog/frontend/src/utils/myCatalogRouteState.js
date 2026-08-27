const RELATIONS = new Set(['responsible', 'favorite', 'following'])

function firstValue(value) {
  return Array.isArray(value) ? value[0] : value
}

function positiveInteger(value, fallback, maximum) {
  const parsed = Number.parseInt(firstValue(value), 10)
  if (!Number.isInteger(parsed) || parsed < 1) return fallback
  return Math.min(parsed, maximum)
}

export function parseMyCatalogRoute(query = {}) {
  const relation = String(firstValue(query.relation) || '').trim()
  return {
    relation: RELATIONS.has(relation) ? relation : 'responsible',
    page: positiveInteger(query.page, 1, Number.MAX_SAFE_INTEGER),
    page_size: positiveInteger(query.page_size, 20, 200)
  }
}

export function buildMyCatalogQuery(state = {}) {
  const parsed = parseMyCatalogRoute(state)
  const query = {}
  if (parsed.relation !== 'responsible') query.relation = parsed.relation
  if (parsed.page !== 1) query.page = String(parsed.page)
  if (parsed.page_size !== 20) query.page_size = String(parsed.page_size)
  return query
}

export function isCanonicalMyCatalogQuery(current = {}, canonical = {}) {
  const normalize = value => Object.fromEntries(Object.entries(value).map(([key, item]) => [key, String(firstValue(item))]).sort())
  return JSON.stringify(normalize(current)) === JSON.stringify(normalize(canonical))
}
