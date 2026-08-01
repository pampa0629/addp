import { navigateConsoleModuleRoute } from '@common-ui'

export function navigateStandardRoute(router, location, options = {}) {
  return navigateConsoleModuleRoute(router, 'standard', location, options)
}
