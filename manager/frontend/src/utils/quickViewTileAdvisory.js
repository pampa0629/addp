export function quickViewTileAdvisoryTimeoutBudgetMS(advisory = {}, quickViewStatus = {}) {
  return Number(advisory?.timeoutBudgetMS || quickViewStatus?.realtime_tile?.timeout_budget_ms || 0)
}

export function quickViewTileAdvisoryAction(advisory = {}, quickViewStatus = {}, fallbackSourceSRID = 0) {
  const recommendation = String(advisory?.recommendation || '').trim()
  const retryPolicy = String(advisory?.retryPolicy || '').trim()
  if (recommendation === 'tile_cache_generation') return 'tile_cache_generation'

  if (recommendation === 'quick_view_optimization' || retryPolicy === 'suppress_tile') {
    const optimization = quickViewStatus?.optimization || {}
    if (optimization.available === true) return 'tile_cache_generation'
    const sourceSRID = Number(quickViewStatus?.render_facts?.source_srid || fallbackSourceSRID || 0)
    return sourceSRID === 3857 ? 'tile_cache_generation' : 'quick_view_optimization'
  }

  return ''
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

export function quickViewTileAdvisoryNoticeKey(action, advisory = {}, quickViewStatus = {}) {
  if (!action) return ''
  return [
    action,
    String(advisory?.recommendation || '').trim(),
    String(advisory?.retryPolicy || '').trim(),
    String(advisory?.performanceMode || '').trim(),
    String(quickViewStatus?.optimization?.target_kind || '').trim()
  ].join('|')
}

export function shouldShowQuickViewTileAdvisoryNotice(
  lastNotice = {},
  action,
  advisory = {},
  quickViewStatus = {},
  now = Date.now(),
  cooldownMS = 60000
) {
  const key = quickViewTileAdvisoryNoticeKey(action, advisory, quickViewStatus)
  if (!key) {
    return { show: false, lastNotice }
  }
  if (lastNotice.key === key && now - Number(lastNotice.at || 0) < cooldownMS) {
    return { show: false, lastNotice }
  }
  return {
    show: true,
    lastNotice: { key, at: now }
  }
}
