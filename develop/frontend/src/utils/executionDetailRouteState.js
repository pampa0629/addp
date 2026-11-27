import { resolveCanonicalTabRouteState } from '@common-ui/utils/recoverableRouteState'

const BASE_TABS = ['result', 'logs', 'inputs']

export function resolveExecutionDetailRouteState(routeQuery = {}, taskType) {
  const { return_to, ...tabQuery } = routeQuery
  const allowedTabs = taskType === undefined || taskType === 'workflow' ? [...BASE_TABS, 'process'] : BASE_TABS

  const state = resolveCanonicalTabRouteState({
    allowedTabs,
    defaultTab: 'result',
    routeQuery: tabQuery
  })
  if (Object.hasOwn(routeQuery, 'return_to')) state.query.return_to = return_to
  return state
}
