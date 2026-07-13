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

export function isModel3DQuickViewSource(previewData = {}, node = {}, selectedNodePath = '') {
  const object = previewData?.object || {}
  const item = object.attributes?.item || {}
  const contentMetadata = object.content?.metadata || {}
  const metadata = previewData?.metadata || {}
  const dataType = String(item.data_type || metadata.data_type || contentMetadata.data_type || '').trim().toLowerCase()
  const format = String(
    item.format ||
    metadata.source_format ||
    metadata.format ||
    contentMetadata.source_format ||
    contentMetadata.format ||
    node.format ||
    node.file_format ||
    ''
  ).trim().toLowerCase()
  const layout = String(item.layout || metadata.layout || contentMetadata.layout || '').trim().toLowerCase()

  if (format === 'osgb_scene' && (!layout || layout === 'whole')) return true
  if (dataType === 'model_3d' && format === 'osgb' && (!layout || layout === 'single')) return true
  if (dataType === 'model_3d' && format === 'gltf' && layout === 'multi') return true
  if (dataType === 'model_3d' && ['fbx', 'obj', 'stl', 'ifc'].includes(format) && (!layout || layout === 'single')) return true
  if (['osgb', 'gltf', 'fbx', 'obj', 'stl', 'ifc'].includes(format)) return true
  return /\.(osgb|gltf|fbx|obj|stl|ifc)$/i.test(String(selectedNodePath || ''))
}

export function resolveQuickViewRenderSource(status = {}) {
  const explicit = normalizeQuickViewRenderSource(status?.render_source || status?.quick_view?.render_source)
  if (explicit) return explicit
  const quickView = status?.quick_view || {}
  if (quickView.flatgeobuf_url || quickView.flatGeobufURL) return 'direct_flatgeobuf'
  const tileURLTemplate = quickView.tile_url_template || quickView.tileURLTemplate
  if (tileURLTemplate) return 'cached_tile'
  return ''
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
  const renderSource = resolveQuickViewRenderSource(status)
  return renderSource === 'direct_flatgeobuf' || isTileQuickViewRenderSource(renderSource)
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

export function shouldUseBackendQuickViewRenderer(activePreviewMode, quickViewStatus, options = {}) {
  if (options.selectedChildPreview === true) return false
  return normalizeQuickViewRenderSource(activePreviewMode) === 'map_quick_view' &&
    quickViewStatus?.can_use_quick_view === true
}
