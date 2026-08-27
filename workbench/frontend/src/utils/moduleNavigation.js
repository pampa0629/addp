import { navigateConsoleModuleRoute } from '@common-ui'

export function navigateWorkbenchRoute(router, location, options = {}) {
  return navigateConsoleModuleRoute(router, 'workbench', location, options)
}
