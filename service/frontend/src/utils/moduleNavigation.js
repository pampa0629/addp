import { navigateConsoleModuleRoute } from '@common-ui'

export function navigateServiceRoute(router, location, options = {}) {
  return navigateConsoleModuleRoute(router, 'service', location, options)
}
