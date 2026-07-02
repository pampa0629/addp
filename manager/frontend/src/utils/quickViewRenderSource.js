const RASTER_QUICK_VIEW_RENDER_SOURCES = new Set([
  'direct_tiff_client',
  'client_cog_render',
  'raster_mosaic_tile'
])

const TILE_QUICK_VIEW_RENDER_SOURCES = new Set([
  'cached_tile',
  'realtime_tile'
])

export function normalizeQuickViewRenderSource(renderSource) {
  return String(renderSource || '').trim()
}

export function isRasterQuickViewRenderSource(renderSource) {
  return RASTER_QUICK_VIEW_RENDER_SOURCES.has(normalizeQuickViewRenderSource(renderSource))
}

export function isTileQuickViewRenderSource(renderSource) {
  return TILE_QUICK_VIEW_RENDER_SOURCES.has(normalizeQuickViewRenderSource(renderSource))
}

export function normalizeQuickViewSourceKind(sourceKind) {
  return String(sourceKind || '').trim()
}

export function isVectorQuickViewSource(status = {}) {
  const sourceKind = normalizeQuickViewSourceKind(status?.source_kind)
  if (sourceKind) return sourceKind === 'vector'
  const renderSource = normalizeQuickViewRenderSource(status?.render_source || status?.quick_view?.render_source)
  return renderSource === 'direct_geojson' || isTileQuickViewRenderSource(renderSource)
}

export function shouldShowQuickViewUnavailableNotice(status = {}) {
  if (!status || status.can_use_quick_view === true) return false
  const sourceKind = normalizeQuickViewSourceKind(status.source_kind)
  if (sourceKind === 'raster' || sourceKind === 'raster_mosaic') return Boolean(status.unavailable_reason)
  if (sourceKind === 'model_3d' || sourceKind === 'gaussian_splat') return false
  return isVectorQuickViewSource(status) && Boolean(status.unavailable_reason)
}

export function hasQuickViewAction(status = {}, action = '') {
  const actions = Array.isArray(status?.available_actions) ? status.available_actions : []
  return actions.includes(action)
}

export function shouldLoadBasicPreview(activePreviewMode, quickViewStatus) {
  return !(normalizeQuickViewRenderSource(activePreviewMode) === 'map_quick_view' &&
    quickViewStatus?.can_use_quick_view === true)
}
