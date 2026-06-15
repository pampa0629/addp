import { computed, ref } from 'vue'

const createEmptyTileStatus = () => ({
  renderSource: '',
  tileCacheId: '',
  cacheStatus: '',
  tileSemanticStatus: '',
  generationTime: '',
  featureCount: null,
  totalFeatureCount: 0,
  loadedTileCount: 0,
  tileStatusCounts: {
    ok: 0,
    empty: 0,
    timeout: 0,
    degraded: 0
  },
  hasCacheHit: false,
  hasCacheMiss: false,
  hasNonEmptyDynamicTile: false,
  error: ''
})

export function useVectorTileRenderStatus({
  t,
  getRenderSource,
  getDefaultTileCacheId,
  rememberTileState
}) {
  const tileStatus = ref(createEmptyTileStatus())

  const normalizedRenderSource = computed(() => String(getRenderSource() || tileStatus.value.renderSource || '').trim())
  const activeTileCacheId = computed(() => String(tileStatus.value.tileCacheId || getDefaultTileCacheId() || '').trim())

  const renderStatusKind = computed(() => {
    if (tileStatus.value.error) return 'error'
    if (tileStatus.value.tileStatusCounts.timeout > 0 || tileStatus.value.tileStatusCounts.degraded > 0) return 'warning'
    if (normalizedRenderSource.value === 'cached_tile' && tileStatus.value.hasCacheHit) return 'cache'
    if (tileStatus.value.hasNonEmptyDynamicTile) return 'dynamic'
    if (normalizedRenderSource.value === 'cached_tile') return 'cache-priority'
    if (tileStatus.value.hasCacheMiss) return 'dynamic'
    if (normalizedRenderSource.value === 'realtime_tile') return 'dynamic'
    return 'unknown'
  })

  const renderStatusClass = computed(() => `is-${renderStatusKind.value}`)

  const renderStatusLabel = computed(() => {
    switch (renderStatusKind.value) {
      case 'cache':
        return t('manager.spatialPreview.renderStatus.cacheHit')
      case 'dynamic':
        return t('manager.spatialPreview.renderStatus.dynamicMvt')
      case 'cache-priority':
        return t('manager.spatialPreview.renderStatus.cachePriority')
      case 'error':
        return t('manager.spatialPreview.renderStatus.tileError')
      case 'warning':
        return t('manager.spatialPreview.renderStatus.tileWarning')
      default:
        return t('manager.spatialPreview.renderStatus.unknown')
    }
  })

  const renderSourceLabel = computed(() => {
    if (normalizedRenderSource.value === 'cached_tile') return t('manager.spatialPreview.renderSource.tileCache')
    if (normalizedRenderSource.value === 'realtime_tile') return t('manager.spatialPreview.renderSource.realtimeTile')
    return normalizedRenderSource.value || '-'
  })

  const renderStatusTooltip = computed(() => {
    const parts = [
      t('manager.spatialPreview.renderStatusTooltip.source', { source: renderSourceLabel.value })
    ]
    if (activeTileCacheId.value) {
      parts.push(t('manager.spatialPreview.renderStatusTooltip.tileCacheResult', { id: activeTileCacheId.value }))
    }
    if (tileStatus.value.hasCacheHit) {
      parts.push(t('manager.spatialPreview.renderStatusTooltip.cacheHit'))
    } else if (tileStatus.value.hasCacheMiss || tileStatus.value.hasNonEmptyDynamicTile) {
      parts.push(t('manager.spatialPreview.renderStatusTooltip.cacheMiss'))
    } else {
      parts.push(t('manager.spatialPreview.renderStatusTooltip.cacheUnknown'))
    }
    if (tileStatus.value.generationTime) {
      parts.push(t('manager.spatialPreview.renderStatusTooltip.generationTime', { time: tileStatus.value.generationTime }))
    }
    const statusParts = Object.entries(tileStatus.value.tileStatusCounts)
      .filter(([, count]) => count > 0)
      .map(([status, count]) => `${status}: ${count}`)
    if (statusParts.length > 0) {
      parts.push(t('manager.spatialPreview.renderStatusTooltip.tileStatus', { status: statusParts.join(', ') }))
    }
    if (tileStatus.value.loadedTileCount > 0) {
      parts.push(t('manager.spatialPreview.renderStatusTooltip.featureCount', { count: tileStatus.value.totalFeatureCount }))
    }
    if (tileStatus.value.error) {
      parts.push(t('manager.spatialPreview.renderStatusTooltip.error', { error: tileStatus.value.error }))
    }
    return parts.join('\n')
  })

  function resetTileStatus() {
    tileStatus.value = createEmptyTileStatus()
  }

  function handleTileLoadEnd(meta) {
    const cacheStatus = String(meta.cacheStatus || '').toUpperCase()
    const semanticStatus = String(meta.tileStatus || '').toLowerCase()
    const featureCount = Number.isFinite(Number(meta.featureCount)) ? Number(meta.featureCount) : 0
    rememberTileState?.({ ...meta, featureCount }, false)
    const tileStatusCounts = { ...tileStatus.value.tileStatusCounts }
    if (Object.prototype.hasOwnProperty.call(tileStatusCounts, semanticStatus)) {
      tileStatusCounts[semanticStatus] += 1
    }
    tileStatus.value = {
      ...tileStatus.value,
      renderSource: meta.renderSource || tileStatus.value.renderSource,
      tileCacheId: meta.tileCacheId || tileStatus.value.tileCacheId,
      cacheStatus: cacheStatus || tileStatus.value.cacheStatus,
      tileSemanticStatus: semanticStatus || tileStatus.value.tileSemanticStatus,
      generationTime: meta.generationTime || tileStatus.value.generationTime,
      featureCount,
      totalFeatureCount: tileStatus.value.totalFeatureCount + featureCount,
      loadedTileCount: tileStatus.value.loadedTileCount + 1,
      tileStatusCounts,
      hasCacheHit: tileStatus.value.hasCacheHit || cacheStatus === 'HIT',
      hasCacheMiss: tileStatus.value.hasCacheMiss || cacheStatus === 'MISS',
      hasNonEmptyDynamicTile: tileStatus.value.hasNonEmptyDynamicTile || (cacheStatus === 'MISS' && featureCount > 0),
      error: ''
    }
  }

  function handleTileLoadError(meta) {
    const semanticStatus = String(meta?.tileStatus || (meta?.oversized ? 'degraded' : '')).toLowerCase()
    rememberTileState?.({ ...meta, tileStatus: semanticStatus }, true)
    const tileStatusCounts = { ...tileStatus.value.tileStatusCounts }
    if (Object.prototype.hasOwnProperty.call(tileStatusCounts, semanticStatus)) {
      tileStatusCounts[semanticStatus] += 1
    }
    tileStatus.value = {
      ...tileStatus.value,
      renderSource: meta?.renderSource || tileStatus.value.renderSource,
      tileCacheId: meta?.tileCacheId || tileStatus.value.tileCacheId,
      tileSemanticStatus: semanticStatus || tileStatus.value.tileSemanticStatus,
      tileStatusCounts,
      error: meta?.cooledDown ? '' : (meta?.error?.message || String(meta?.error || ''))
    }
  }

  return {
    tileStatus,
    renderStatusClass,
    renderStatusLabel,
    renderStatusTooltip,
    resetTileStatus,
    handleTileLoadEnd,
    handleTileLoadError
  }
}
