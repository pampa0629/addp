export function quickViewTileAdvisoryTimeoutBudgetMS(advisory = {}, quickViewStatus = {}) {
  return Number(advisory?.timeoutBudgetMS || quickViewStatus?.realtime_tile?.timeout_budget_ms || 0)
}

export function quickViewTileAdvisoryMessage(t, action, advisory = {}, quickViewStatus = {}) {
  const timeoutBudgetMS = quickViewTileAdvisoryTimeoutBudgetMS(advisory, quickViewStatus)
  const params = {
    timeout: timeoutBudgetMS > 0 ? Math.round(timeoutBudgetMS / 1000) : ''
  }
  if (action === 'tile_cache_generation') {
    return t(timeoutBudgetMS > 0
      ? 'manager.spatialPreview.tileTimeoutCacheRecommendedWithBudget'
      : 'manager.spatialPreview.tileTimeoutCacheRecommended', params)
  }
  if (action === 'quick_view_optimization') {
    return t(timeoutBudgetMS > 0
      ? 'manager.spatialPreview.tileTimeoutOptimizationRecommendedWithBudget'
      : 'manager.spatialPreview.tileTimeoutOptimizationRecommended', params)
  }
  return ''
}
