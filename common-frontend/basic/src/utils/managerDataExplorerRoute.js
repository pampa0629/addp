export function buildManagerDataExplorerRoute(locator) {
  const normalizedLocator = String(locator || '').trim()
  if (!normalizedLocator) return '/manager/data-explorer'

  const query = new URLSearchParams({ locator: normalizedLocator })
  return `/manager/data-explorer?${query.toString()}`
}
