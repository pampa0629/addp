import { navigateConsoleModuleRoute } from '@common-ui'

export function navigateSystemRoute(router, location, options = {}) {
  return navigateConsoleModuleRoute(router, 'system', location, options)
}
