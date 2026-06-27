export function rasterGeoTIFFSourceOptions(renderSource, token = '') {
  const normalizedRenderSource = String(renderSource || '').trim()
  const isCOGRender = normalizedRenderSource === 'client_cog_render'
  const headers = {}
  const cleanedToken = String(token || '').trim()
  if (cleanedToken) {
    headers.Authorization = `Bearer ${cleanedToken}`
  }
  return {
    allowFullFile: !isCOGRender,
    blockSize: isCOGRender ? 262144 : 65536,
    cacheSize: isCOGRender ? 128 : 100,
    headers
  }
}

export function rasterGeoTIFFProjectionFromQuickView(quickViewInfo) {
  if (!quickViewInfo || typeof quickViewInfo !== 'object') return ''
  const srid = Number(quickViewInfo.extent_srid || quickViewInfo.source_srid || 0)
  if (srid === 4326 || srid === 3857) {
    return `EPSG:${srid}`
  }
  return ''
}
