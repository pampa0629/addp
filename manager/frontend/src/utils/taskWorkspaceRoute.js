import { resolveCanonicalTabRouteState } from '@common-ui/utils/recoverableRouteState'

const WORKSPACE_TABS = ['tasks', 'results']
const NON_NEGATIVE_INTEGER_KEYS = new Set(['source_size_bytes'])

function queryValue(value) {
  const firstValue = Array.isArray(value) ? value[0] : value
  return firstValue == null ? '' : String(firstValue).trim()
}

function normalizePositiveInteger(value) {
  if (!/^\d+$/.test(value)) return ''
  const normalized = BigInt(value)
  return normalized > 0n ? String(normalized) : ''
}

function normalizeNonNegativeInteger(value) {
  if (!/^\d+$/.test(value)) return ''
  return String(BigInt(value))
}

function normalizePreservedValue(key, value) {
  const normalized = queryValue(value)
  if (!normalized) return ''
  if (key === 'create') return normalized === '1' ? '1' : ''
  if (key === 'id' || key.endsWith('_id')) return normalizePositiveInteger(normalized)
  if (NON_NEGATIVE_INTEGER_KEYS.has(key)) return normalizeNonNegativeInteger(normalized)
  return normalized
}

export function resolveManagerTaskWorkspaceRouteState({
  routeQuery = {},
  allowedQuery = [],
  allowedQueryByTab = null,
  taskIDScope = 'all'
} = {}) {
  if (!['all', 'results'].includes(taskIDScope)) {
    throw new Error('task ID scope must be all or results')
  }
  const requestedTab = queryValue(routeQuery.tab)
  const routeQueryKeys = allowedQueryByTab
    ? (allowedQueryByTab[requestedTab === 'results' ? 'results' : 'tasks'] || [])
    : allowedQuery
  const preservedQuery = {}
  for (const key of routeQueryKeys) {
    if (key === 'tab') continue
    if (key === 'task_id' && taskIDScope === 'results' && requestedTab !== 'results') continue
    const value = normalizePreservedValue(key, routeQuery[key])
    if (value) preservedQuery[key] = value
  }

  return resolveCanonicalTabRouteState({
    allowedTabs: WORKSPACE_TABS,
    defaultTab: 'tasks',
    routeQuery,
    preservedQuery
  })
}
