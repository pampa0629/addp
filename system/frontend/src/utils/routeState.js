const AUDIT_TABS = new Set(['platform-audit', 'tenant-audit'])
const AUDIT_QUERY_KEYS = ['event_name', 'result', 'risk_level', 'module_name', 'entity_type', 'entity_id', 'page']
const AUDIT_RESULTS = new Set(['succeeded', 'failed', 'denied', 'ignored'])
const AUDIT_RISK_LEVELS = new Set(['low', 'medium', 'high', 'critical'])

function queriesEqual(left, right) {
  const rightKeys = Object.keys(right)
  return Object.keys(left).length === rightKeys.length &&
    rightKeys.every(key => String(left[key] || '') === String(right[key] || ''))
}

export function resolveIAMRouteState(availableTabKeys, routeQuery = {}) {
  const tabs = availableTabKeys.map(key => String(key))
  const defaultTab = tabs[0] || ''
  const requestedTab = String(routeQuery.tab || '').trim()
  const activeTab = tabs.includes(requestedTab) ? requestedTab : defaultTab
  const query = activeTab && activeTab !== defaultTab ? { tab: activeTab } : {}

  if (AUDIT_TABS.has(activeTab)) {
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
