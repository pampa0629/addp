import { navigateConsoleModuleRoute } from '@common-ui'

export function navigateAssetRoute(router, location, options = {}) {
  return navigateConsoleModuleRoute(router, 'asset', location, options)
}
