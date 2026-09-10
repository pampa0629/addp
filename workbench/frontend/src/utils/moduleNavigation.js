import { navigateConsoleModuleRoute, resolveConsoleRouteUrl } from '@common-ui'
import { buildDataApplicationRuntimeRoute } from './dataApplicationDelivery.mjs'

export function navigateWorkbenchRoute(router, location, options = {}) {
  return navigateConsoleModuleRoute(router, 'workbench', location, options)
}

export function dataApplicationRuntimeURL(applicationID, presetKey = '') {
  const route = buildDataApplicationRuntimeRoute(applicationID, presetKey)
  if (!route) throw new Error('data application ID is required')
  return resolveConsoleRouteUrl(route)
}

export function openDataApplicationRuntime(applicationID, presetKey = '') {
  const url = dataApplicationRuntimeURL(applicationID, presetKey)
  if (!url || typeof window === 'undefined') return false
  window.open(url, '_blank', 'noopener,noreferrer')
  return true
}
