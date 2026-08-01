import { navigateConsoleModuleRoute } from '@common-ui'

export function navigateAgentRoute(router, location, options = {}) {
  return navigateConsoleModuleRoute(router, 'agent', location, options)
}
