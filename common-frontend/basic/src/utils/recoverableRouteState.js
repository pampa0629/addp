function queryValue(value) {
  const firstValue = Array.isArray(value) ? value[0] : value
  return firstValue == null ? '' : String(firstValue).trim()
}

function queriesEqual(left, right) {
  const rightKeys = Object.keys(right)
  return Object.keys(left).length === rightKeys.length &&
    rightKeys.every(key => !Array.isArray(left[key]) && queryValue(left[key]) === queryValue(right[key]))
}

export function resolveCanonicalTabRouteState({
  allowedTabs,
  defaultTab,
  routeQuery = {},
  preservedQuery = {}
}) {
  const allowed = new Set(allowedTabs.map(tab => String(tab)))
  if (!allowed.has(defaultTab)) {
    throw new Error('default tab must be included in allowed tabs')
  }

  const requestedTab = queryValue(routeQuery.tab)
  const tab = allowed.has(requestedTab) ? requestedTab : defaultTab
  const query = {}

  for (const [key, value] of Object.entries(preservedQuery)) {
    if (key === 'tab') continue
    const normalizedValue = queryValue(value)
    if (normalizedValue) query[key] = normalizedValue
  }
  if (tab !== defaultTab) query.tab = tab

  return {
    tab,
    query,
    changed: !queriesEqual(routeQuery, query)
  }
}
