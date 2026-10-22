import { get as getProjection, getTransform, transformExtent } from 'ol/proj.js'

export function rasterWebMercatorExtent(quickViewInfo) {
  const extent = quickViewInfo?.extent
  const srid = Number(quickViewInfo?.extent_srid || 0)
  if (!Array.isArray(extent) || extent.length !== 4 || !Number.isInteger(srid) || srid <= 0) return null
  const bounds = extent.map(Number)
  if (!bounds.every(Number.isFinite) || bounds[2] <= bounds[0] || bounds[3] <= bounds[1]) return null
  const projection = getProjection(`EPSG:${srid}`)
  if (!projection || !getTransform(projection, 'EPSG:3857')) return null
  const transformed = transformExtent(bounds, projection, 'EPSG:3857', 8)
  return transformed.every(Number.isFinite) ? transformed : null
}

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
