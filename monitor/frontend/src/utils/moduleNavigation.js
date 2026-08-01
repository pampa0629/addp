import { navigateConsoleModuleRoute } from '@common-ui'

export function navigateMonitorRoute(router, location, options = {}) {
  return navigateConsoleModuleRoute(router, 'monitor', location, options)
}
