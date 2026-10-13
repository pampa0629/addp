import { resolveCanonicalTabRouteState } from '@common-ui/utils/recoverableRouteState'

const BASE_TABS = ['result', 'logs', 'inputs']

export function resolveExecutionDetailRouteState(routeQuery = {}) {
  const allowedTabs = BASE_TABS

  return resolveCanonicalTabRouteState({
    allowedTabs,
    defaultTab: 'result',
    routeQuery
  })
}
