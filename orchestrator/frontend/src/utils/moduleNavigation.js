import { navigateConsoleModuleRoute } from '@common-ui'

export function navigateOrchestratorRoute(router, location, options = {}) {
  return navigateConsoleModuleRoute(router, 'orchestrator', location, options)
}
