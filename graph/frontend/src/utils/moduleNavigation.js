import { navigateConsoleModuleRoute } from '@common-ui'

export function navigateGraphRoute(router, location, options = {}) {
  return navigateConsoleModuleRoute(router, 'graph', location, options)
}
