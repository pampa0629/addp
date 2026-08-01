import { navigateConsoleModuleRoute } from '@common-ui'

export function navigateTransferRoute(router, location, options = {}) {
  return navigateConsoleModuleRoute(router, 'transfer', location, options)
}
