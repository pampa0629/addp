export const DASHBOARD_SCOPE_ALL = 'all'
export const DASHBOARD_SCOPE_APPLICATION = 'application'
export const DASHBOARD_SCOPE_ASSET_PREFIX = 'asset:'

export function dashboardStatsParams(scope) {
  if (scope === DASHBOARD_SCOPE_APPLICATION) return { type_code: 'application' }
  if (scope.startsWith(DASHBOARD_SCOPE_ASSET_PREFIX)) {
    return {
      type_code: 'application',
      asset_id: Number(scope.slice(DASHBOARD_SCOPE_ASSET_PREFIX.length))
    }
  }
  return {}
}
