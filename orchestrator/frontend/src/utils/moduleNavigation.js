import { navigateConsoleModuleRoute, buildOrchestrationListRoute, resolveOrchestrationTaskFilter } from '@common-ui'

export function navigateOrchestratorRoute(router, location, options = {}) {
  return navigateConsoleModuleRoute(router, 'orchestrator', location, options)
}

export function orchestrationListLocation(query) {
  const canonical = new URL(buildOrchestrationListRoute(resolveOrchestrationTaskFilter(query) || {}), 'https://route.invalid')
  return { path: '/orchestrations', query: Object.fromEntries(canonical.searchParams) }
}
