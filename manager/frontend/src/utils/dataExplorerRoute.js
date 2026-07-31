export const DATA_EXPLORER_DEFAULT_TAB = 'preview'
export const DATA_EXPLORER_TABS = new Set(['preview', 'profile', 'attributes'])

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

export function buildDataExplorerConsoleRoute(locator, tab = DATA_EXPLORER_DEFAULT_TAB) {
  const query = buildDataExplorerQuery(locator, tab)
  const search = new URLSearchParams()
  if (query.locator) search.set('locator', query.locator)
  if (query.tab) search.set('tab', query.tab)
  const queryString = search.toString()
  return queryString
    ? `/manager/data-explorer?${queryString}`
    : '/manager/data-explorer'
}
