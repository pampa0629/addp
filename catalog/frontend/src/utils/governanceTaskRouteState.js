const ALLOWED_STATUSES = new Set(['open', 'resolved'])
const UUID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/

function firstValue(value) {
  return Array.isArray(value) ? value[0] : value
}

function positiveInteger(value, fallback, maximum) {
  const parsed = Number.parseInt(firstValue(value), 10)
  if (!Number.isInteger(parsed) || parsed < 1) return fallback
  return Math.min(parsed, maximum)
}

export function parseGovernanceTaskRoute(query = {}) {
  const rawStatus = String(firstValue(query.status) || '').trim()
  const rawEntryID = String(firstValue(query.entry_id) || '').trim()
  return {
    status: ALLOWED_STATUSES.has(rawStatus) ? rawStatus : 'open',
    entry_id: UUID_PATTERN.test(rawEntryID) ? rawEntryID : '',
    page: positiveInteger(query.page, 1, Number.MAX_SAFE_INTEGER),
    page_size: positiveInteger(query.page_size, 20, 200)
  }
}

export function buildGovernanceTaskQuery(state = {}) {
  const parsed = parseGovernanceTaskRoute(state)
  const query = {}
  if (parsed.status !== 'open') query.status = parsed.status
  if (parsed.entry_id) query.entry_id = parsed.entry_id
  if (parsed.page !== 1) query.page = String(parsed.page)
  if (parsed.page_size !== 20) query.page_size = String(parsed.page_size)
  return query
}

export function buildGovernanceEntryCandidateQuery(search = '') {
  const normalizedSearch = String(search || '').trim()
  return {
    view: 'inventory',
    ...(normalizedSearch ? { search: normalizedSearch } : {}),
    page: 1,
    page_size: 20
  }
}

export function isCanonicalGovernanceTaskQuery(current = {}, canonical = {}) {
  const normalize = value => Object.fromEntries(Object.entries(value).map(([key, item]) => [key, String(firstValue(item))]).sort())
  return JSON.stringify(normalize(current)) === JSON.stringify(normalize(canonical))
}
