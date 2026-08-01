import { navigateConsoleModuleRoute } from '@common-ui'

export function navigateQualityRoute(router, location, options = {}) {
  return navigateConsoleModuleRoute(router, 'quality', location, options)
}
