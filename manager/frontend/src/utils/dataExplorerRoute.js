import { resolveCanonicalTabRouteState } from '@common-ui/utils/recoverableRouteState'

export const DATA_EXPLORER_DEFAULT_TAB = 'preview'
export const DATA_EXPLORER_TABS = new Set(['preview', 'profile', 'attributes', 'lineage'])

const queryValue = value => {
  const firstValue = Array.isArray(value) ? value[0] : value
  return firstValue == null ? '' : String(firstValue).trim()
}

export function normalizeDataExplorerTab(tab) {
  const normalized = String(tab || '').trim().toLowerCase()
  return DATA_EXPLORER_TABS.has(normalized) ? normalized : DATA_EXPLORER_DEFAULT_TAB
}

export function buildDataExplorerQuery(locator, tab = DATA_EXPLORER_DEFAULT_TAB) {
  const normalizedLocator = String(locator || '').trim()
  const normalizedTab = normalizeDataExplorerTab(tab)
  return {
    locator: normalizedLocator || undefined,
    tab: normalizedLocator && normalizedTab !== DATA_EXPLORER_DEFAULT_TAB ? normalizedTab : undefined
  }
}

export function resolveDataExplorerRouteState(routeQuery = {}) {
  const locator = queryValue(routeQuery.locator)
  const rawTab = queryValue(routeQuery.tab)
  const normalizedTab = rawTab.toLowerCase()
  const normalizedRouteQuery = { ...routeQuery }
  if (normalizedTab) normalizedRouteQuery.tab = normalizedTab
  else delete normalizedRouteQuery.tab
  const routeState = resolveCanonicalTabRouteState({
    allowedTabs: locator ? [...DATA_EXPLORER_TABS] : [DATA_EXPLORER_DEFAULT_TAB],
    defaultTab: DATA_EXPLORER_DEFAULT_TAB,
    routeQuery: normalizedRouteQuery,
    preservedQuery: locator ? { locator } : {}
  })

  return {
    ...routeState,
    locator,
    changed: routeState.changed || Array.isArray(routeQuery.tab) || rawTab !== normalizedTab
  }
}

export function buildCatalogEntryConsoleRoute(entryId) {
  const normalizedEntryId = String(entryId || '').trim()
  return normalizedEntryId
    ? `/catalog/entries/${encodeURIComponent(normalizedEntryId)}`
    : '/catalog/entries'
}
