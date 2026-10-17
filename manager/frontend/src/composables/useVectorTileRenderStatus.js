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
  rememberTileState,
  getVisibleTileKeys
}) {
  const tileStatus = ref(createEmptyTileStatus())
  const observations = new Map()
  const maxStatusObservations = 512
  let sourceGeneration = 0

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
    sourceGeneration += 1
    observations.clear()
    tileStatus.value = createEmptyTileStatus()
  }

  function refreshTileStatus() {
    const visible = new Set(getVisibleTileKeys(observations.keys()))
    const state = createEmptyTileStatus()
    for (const key of visible) {
      const entry = observations.get(key)
      if (!entry) continue
      const { meta, hasError } = entry
      const cacheStatus = String(meta.cacheStatus || '').toUpperCase()
      const semanticStatus = String(meta.tileStatus || (hasError && meta.oversized ? 'degraded' : '')).toLowerCase()
      const featureCount = hasError ? 0 : Number.isFinite(Number(meta.featureCount)) ? Number(meta.featureCount) : 0
      state.renderSource = meta.renderSource || state.renderSource
      state.tileCacheId = meta.tileCacheId || state.tileCacheId
      state.cacheStatus = cacheStatus || state.cacheStatus
      state.tileSemanticStatus = semanticStatus || state.tileSemanticStatus
      state.generationTime = meta.generationTime || state.generationTime
      if (Object.prototype.hasOwnProperty.call(state.tileStatusCounts, semanticStatus)) state.tileStatusCounts[semanticStatus] += 1
      state.hasCacheHit ||= cacheStatus === 'HIT'
      state.hasCacheMiss ||= cacheStatus === 'MISS'
      state.hasNonEmptyDynamicTile ||= cacheStatus === 'MISS' && featureCount > 0
      if (hasError) {
        if (!meta.cooledDown && !meta.suppressed) state.error = meta.error?.message || String(meta.error || '')
      } else {
        state.featureCount = featureCount
        state.totalFeatureCount += featureCount
        state.loadedTileCount += 1
      }
    }
    tileStatus.value = state
  }

  function rememberObservation(meta, hasError) {
    if (!meta.tileKey) return
    observations.delete(meta.tileKey)
    observations.set(meta.tileKey, { meta, hasError })
    while (observations.size > maxStatusObservations) observations.delete(observations.keys().next().value)
    rememberTileState?.(meta, hasError)
    refreshTileStatus()
  }

  function beginTileSource() {
    resetTileStatus()
    const generation = sourceGeneration
    return {
      onTileLoadEnd: meta => {
        if (generation === sourceGeneration) rememberObservation(meta, false)
      },
      onTileLoadError: meta => {
        if (generation === sourceGeneration) rememberObservation(meta, true)
      }
    }
  }

  return {
    tileStatus,
    renderStatusClass,
    renderStatusLabel,
    renderStatusTooltip,
    resetTileStatus,
    refreshTileStatus,
    beginTileSource
  }
}
