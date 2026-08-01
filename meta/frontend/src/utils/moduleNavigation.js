import { navigateConsoleModuleRoute } from '@common-ui'

export function navigateMetaRoute(router, location, options = {}) {
  return navigateConsoleModuleRoute(router, 'meta', location, options)
}
