export function isCachedTileRenderSource(renderSource) {
  return String(renderSource || '').trim() === 'cached_tile'
}

export function shouldWarnVectorTileMaxZoom(renderSource) {
  return isCachedTileRenderSource(renderSource)
}

export function vectorTileMaxZoomWarningKey(renderSource) {
  return shouldWarnVectorTileMaxZoom(renderSource)
    ? 'manager.vectorTile.zoomTooHighCached'
    : ''
}

export function vectorTileSourceMaxZoom(renderSource, configuredMaxZoom, fallbackMaxZoom = 22) {
  const maxZoom = Number(configuredMaxZoom)
  if (isCachedTileRenderSource(renderSource) && Number.isFinite(maxZoom) && maxZoom > 0) {
    return maxZoom
  }
  return fallbackMaxZoom
}
