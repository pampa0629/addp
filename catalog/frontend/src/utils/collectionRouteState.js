const MAX_INT64 = 9223372036854775807n

function firstValue(value) {
  return Array.isArray(value) ? value[0] : value
}

function positiveInteger(value, fallback, maximum) {
  const parsed = Number.parseInt(firstValue(value), 10)
  if (!Number.isInteger(parsed) || parsed < 1) return fallback
  return Math.min(parsed, maximum)
}

function canonicalPositiveID(value) {
  const normalized = String(firstValue(value) || '').trim()
  if (!/^[1-9]\d*$/.test(normalized)) return ''
  try {
    return BigInt(normalized) <= MAX_INT64 ? normalized : ''
  } catch {
    return ''
  }
}

export function parseCollectionRoute(query = {}) {
  return {
    project_group_id: canonicalPositiveID(query.project_group_id),
    page: positiveInteger(query.page, 1, Number.MAX_SAFE_INTEGER),
    page_size: positiveInteger(query.page_size, 20, 200)
  }
}

export function buildCollectionQuery(state = {}) {
  const parsed = parseCollectionRoute(state)
  const query = {}
  if (parsed.project_group_id) query.project_group_id = parsed.project_group_id
  if (parsed.page !== 1) query.page = String(parsed.page)
  if (parsed.page_size !== 20) query.page_size = String(parsed.page_size)
  return query
}

export function isCanonicalCollectionQuery(current = {}, canonical = {}) {
  const normalize = value => Object.fromEntries(Object.entries(value).map(([key, item]) => [key, String(firstValue(item))]).sort())
  return JSON.stringify(normalize(current)) === JSON.stringify(normalize(canonical))
}
