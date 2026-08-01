import { resolveCanonicalTabRouteState } from '@common-ui/utils/recoverableRouteState'

export function resolveMonitorTabRouteState(routeQuery, allowedTabs, defaultTab) {
  return resolveCanonicalTabRouteState({
    allowedTabs,
    defaultTab,
    routeQuery
  })
}
