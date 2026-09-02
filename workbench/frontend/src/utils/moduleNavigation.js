import { navigateConsoleModuleRoute, resolveConsoleRouteUrl } from '@common-ui'

export function navigateWorkbenchRoute(router, location, options = {}) {
  return navigateConsoleModuleRoute(router, 'workbench', location, options)
}

export function openDataApplicationRuntime(applicationID) {
  const id = String(applicationID || '').trim()
  if (!id) throw new Error('data application ID is required')
  const url = resolveConsoleRouteUrl(`/data-apps/${encodeURIComponent(id)}`)
  if (!url || typeof window === 'undefined') return false
  window.open(url, '_blank', 'noopener,noreferrer')
  return true
}
