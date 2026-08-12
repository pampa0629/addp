const normalized = (value) => String(value || '').trim().toLowerCase()

export function isVectorTileObjectPreview(data = {}) {
  const content = data.object?.content || {}
  const metadata = content.metadata || {}
  return normalized(content.frontend_renderer || metadata.frontend_renderer) === 'vector_tile'
}

export function vectorTileObjectPreviewProps(data = {}, fallbackLocator = '') {
  const content = data.object?.content || {}
  const metadata = content.metadata || {}
  return {
    locator: metadata.locator || fallbackLocator || '',
    tileUrlTemplate: content.url || metadata.tile_url_template || '',
    tileRenderInfo: metadata,
    renderSource: metadata.render_source || 'business_pmtiles'
  }
}
