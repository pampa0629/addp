import { resolveCanonicalTabRouteState } from '@common-ui/utils/recoverableRouteState'

const BASE_TABS = ['result', 'logs', 'inputs']

export function resolveExecutionDetailRouteState(routeQuery = {}, executionStatus = '') {
  const allowedTabs = executionStatus === 'failed'
    ? [...BASE_TABS, 'error']
    : BASE_TABS

  return resolveCanonicalTabRouteState({
    allowedTabs,
    defaultTab: 'result',
    routeQuery
  })
}
