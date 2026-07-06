export function tileCacheOptimizationAdvice(config = {}) {
  if (!isVectorMaterializedViewSource(config)) {
    return emptyAdvice()
  }
  if (config.optimization_available) {
    const external = config.optimization_target_kind === 'external_3857_materialized_view'
    return {
      visible: true,
      type: 'success',
      actionVisible: false,
      titleKey: external
        ? 'manager.tileCache.externalOptimizationTargetReadyTitle'
        : 'manager.tileCache.optimizationTargetReadyTitle',
      messageKey: external
        ? 'manager.tileCache.externalOptimizationTargetReady'
        : 'manager.tileCache.optimizationTargetReady'
    }
  }
  if (config.optimization_status === 'stale') {
    return {
      visible: true,
      type: 'warning',
      actionVisible: true,
      titleKey: 'manager.tileCache.optimizationTargetStaleTitle',
      messageKey: 'manager.tileCache.optimizationTargetStale'
    }
  }
  const sourceSRID = Number(config.source_srid || 0)
  if (config.optimization_recommended && sourceSRID !== 3857) {
    return {
      visible: true,
      type: 'warning',
      actionVisible: true,
      titleKey: 'manager.tileCache.optimizationRecommendedTitle',
      messageKey: 'manager.tileCache.optimizationRecommended'
    }
  }
  return {
    ...emptyAdvice()
  }
}

function isVectorMaterializedViewSource(config = {}) {
  const sourceKind = String(config.source_kind || '').trim().toLowerCase()
  if (sourceKind) return sourceKind === 'table'
  return Boolean(String(config.source_schema || '').trim() && String(config.source_table || '').trim())
}

function emptyAdvice() {
  return {
    visible: false,
    titleKey: '',
    messageKey: '',
    type: 'warning',
    actionVisible: true
  }
}
