import { resolveCanonicalTabRouteState } from '@common-ui'

export const MODULE_INSTANCE_ROLES = ['backend', 'worker', 'scheduler', 'ingress']
export const MODULE_INSTANCE_PERIODS = [
  { value: '15m', minutes: 15, label: 'system.module.query.periods.m15' },
  { value: '30m', minutes: 30, label: 'system.module.query.periods.m30' },
  { value: '1h', minutes: 60, label: 'system.module.query.periods.h1' },
  { value: '6h', minutes: 360, label: 'system.module.query.periods.h6' },
  { value: '12h', minutes: 720, label: 'system.module.query.periods.h12' },
  { value: '24h', minutes: 1440, label: 'system.module.query.periods.h24' },
  { value: '7d', minutes: 10080, label: 'system.module.query.periods.d7' }
]

function singleQueryValue(value) {
  return typeof value === 'string' ? value.trim() : ''
}

function queryTime(value) {
  const text = singleQueryValue(value)
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(text)) return ''
  const time = new Date(text)
  const calendar = new Date(`${text.slice(0, 10)}T00:00:00Z`)
  if (Number.isNaN(time.getTime()) || Number.isNaN(calendar.getTime()) ||
    calendar.toISOString().slice(0, 10) !== text.slice(0, 10) || Number(text.slice(11, 13)) > 23) return ''
  return time.toISOString()
}

export function resolveModulesRouteState(routeQuery = {}) {
  const instances = singleQueryValue(routeQuery.tab) === 'instances'
  const filters = {
    moduleName: '', registeredHost: '', nodeName: '', role: '', status: 'up',
    timeBasis: 'registered', period: 'all', from: '', to: '', page: 1, pageSize: 10
  }
  const preservedQuery = {}
  if (instances) {
    for (const [key, field] of [['module_name', 'moduleName'], ['registered_host', 'registeredHost'], ['node_name', 'nodeName']]) {
      const value = singleQueryValue(routeQuery[key])
      filters[field] = value
      if (value) preservedQuery[key] = value
    }
    const role = singleQueryValue(routeQuery.role)
    if (MODULE_INSTANCE_ROLES.includes(role)) {
      filters.role = role
      preservedQuery.role = role
    }
    const status = singleQueryValue(routeQuery.status)
    if (status === 'down' || status === 'all') {
      filters.status = status === 'all' ? '' : status
      preservedQuery.status = status
    }
    if (singleQueryValue(routeQuery.time_basis) === 'offline') {
      filters.timeBasis = 'offline'
      preservedQuery.time_basis = 'offline'
    }
    const period = singleQueryValue(routeQuery.time_period)
    if (MODULE_INSTANCE_PERIODS.some(item => item.value === period)) {
      filters.period = period
      preservedQuery.time_period = period
    } else if (period === 'custom') {
      const from = queryTime(routeQuery.time_from)
      const to = queryTime(routeQuery.time_to)
      if (from && to && from < to) {
        Object.assign(filters, { period, from, to })
        Object.assign(preservedQuery, { time_period: period, time_from: from, time_to: to })
      }
    }
    const page = Number(singleQueryValue(routeQuery.page))
    const pageSize = Number(singleQueryValue(routeQuery.page_size))
    if (Number.isSafeInteger(page) && page > 1) {
      filters.page = page
      preservedQuery.page = String(page)
    }
    if ([20, 50, 100].includes(pageSize)) {
      filters.pageSize = pageSize
      preservedQuery.page_size = String(pageSize)
    }
  }
  const state = resolveCanonicalTabRouteState({
    allowedTabs: ['overview', 'instances'], defaultTab: 'overview',
    routeQuery: Array.isArray(routeQuery.tab) ? { ...routeQuery, tab: '' } : routeQuery, preservedQuery
  })
  const queryKeys = Object.keys(state.query)
  const changed = Object.keys(routeQuery).length !== queryKeys.length || queryKeys.some(key => routeQuery[key] !== state.query[key])
  return { ...state, changed, filters }
}

export function buildModuleInstancesQuery(filters) {
  return resolveModulesRouteState({
    tab: 'instances', module_name: filters.moduleName, registered_host: filters.registeredHost,
    node_name: filters.nodeName, role: filters.role,
    status: filters.status === '' ? 'all' : filters.status,
    time_basis: filters.timeBasis, time_period: filters.period, time_from: filters.from, time_to: filters.to,
    page: String(filters.page), page_size: String(filters.pageSize)
  }).query
}

const AUDIT_QUERY_KEYS = ['event_name', 'result', 'risk_level', 'module_name', 'principal_id', 'principal_type', 'entity_type', 'entity_id', 'page']
const AUDIT_RESULTS = new Set(['succeeded', 'failed', 'denied', 'ignored'])
const AUDIT_RISK_LEVELS = new Set(['low', 'medium', 'high', 'critical'])
const PRINCIPAL_TYPES = new Set(['user', 'service_principal'])

function queriesEqual(left, right) {
  const rightKeys = Object.keys(right)
  return Object.keys(left).length === rightKeys.length &&
    rightKeys.every(key => String(left[key] || '') === String(right[key] || ''))
}

export function resolveIAMCategoryRouteState(availableTabKeys, routeQuery = {}) {
  const tabs = availableTabKeys.map(key => String(key))
  const defaultTab = tabs[0] || ''
  const requestedTab = String(routeQuery.tab || '').trim()
  const activeTab = tabs.includes(requestedTab) ? requestedTab : defaultTab
  const query = activeTab && activeTab !== defaultTab ? { tab: activeTab } : {}

  if (activeTab === 'audit') {
    for (const key of AUDIT_QUERY_KEYS) {
      const value = String(routeQuery[key] || '').trim()
      if (!value) continue
      if (key === 'page') {
        const page = Number(value)
        if (Number.isInteger(page) && page > 1) query.page = String(page)
        continue
      }
      if (key === 'result' && !AUDIT_RESULTS.has(value)) continue
      if (key === 'risk_level' && !AUDIT_RISK_LEVELS.has(value)) continue
      if (key === 'principal_type' && !PRINCIPAL_TYPES.has(value)) continue
      query[key] = value
    }
  }

  return {
    activeTab,
    query,
    changed: !queriesEqual(routeQuery, query)
  }
}

export function resolveEngineDetailRouteState(availableTabs, routeQuery = {}) {
  const tabs = availableTabs.map(tab => String(tab))
  const requestedTab = String(routeQuery.tab || '').trim()
  const activeTab = tabs.includes(requestedTab) ? requestedTab : 'basic'
  const query = activeTab === 'basic' ? {} : { tab: activeTab }

  return {
    activeTab,
    query,
    changed: !queriesEqual(routeQuery, query)
  }
}
