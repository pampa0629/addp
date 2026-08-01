import { navigateConsoleModuleRoute } from '@common-ui'

export function navigateManagerRoute(router, location, options = {}) {
  return navigateConsoleModuleRoute(router, 'manager', location, options)
}
