import { navigateConsoleModuleRoute } from '@common-ui'

export function navigateModelRoute(router, location, options = {}) {
  return navigateConsoleModuleRoute(router, 'modeling', location, options)
}
