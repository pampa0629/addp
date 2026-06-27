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

export function shouldLoadBasicPreview(activePreviewMode, quickViewStatus) {
  return !(normalizeQuickViewRenderSource(activePreviewMode) === 'map_quick_view' &&
    quickViewStatus?.can_use_quick_view === true)
}
