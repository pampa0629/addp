export const DEFAULT_QUICK_VIEW_TILE_BUDGET = 10_000

const WEB_MERCATOR_HALF_WORLD = 20037508.34
const WEB_MERCATOR_MAX_LAT = 85.0511287798066

export function calculateTileRangeEstimate({ extent, extentSRID, minZoom, maxZoom }) {
  const normalized = normalizeTileEstimateExtent(extent, extentSRID)
  const startZoom = Number(minZoom)
  const endZoom = Number(maxZoom)
  if (!normalized || !Number.isInteger(startZoom) || !Number.isInteger(endZoom) || startZoom < 0 || endZoom < startZoom || endZoom > 22) {
    return { supported: false, tileCount: 0 }
  }

  let tileCount = 0
  for (let zoom = startZoom; zoom <= endZoom; zoom += 1) {
    tileCount += tileCountAtZoom(normalized, zoom)
  }
  return { supported: true, tileCount }
}

export function isZoomAboveRecommendation(maxZoom, recommendedMaxZoom) {
  const current = Number(maxZoom)
  const recommended = Number(recommendedMaxZoom)
  return Number.isFinite(current) && Number.isFinite(recommended) && recommended > 0 && current > recommended
}

function normalizeTileEstimateExtent(extent, extentSRID) {
  if (!Array.isArray(extent) || extent.length !== 4) return null
  let [minX, minY, maxX, maxY] = extent.map(Number)
  if (![minX, minY, maxX, maxY].every(Number.isFinite) || minX >= maxX || minY >= maxY) return null

  const srid = Number(extentSRID || 4326)
  if (srid === 3857) {
    minX = minX / WEB_MERCATOR_HALF_WORLD * 180
    maxX = maxX / WEB_MERCATOR_HALF_WORLD * 180
    minY = webMercatorYToLatitude(minY)
    maxY = webMercatorYToLatitude(maxY)
  } else if (srid !== 4326) {
    return null
  }
  return [minX, minY, maxX, maxY]
}

function webMercatorYToLatitude(y) {
  return (2 * Math.atan(Math.exp(y / WEB_MERCATOR_HALF_WORLD * Math.PI)) - Math.PI / 2) * 180 / Math.PI
}

function tileCountAtZoom([minLon, minLat, maxLon, maxLat], zoom) {
  const [minTileX, maxTileY] = lonLatToTile(minLon, minLat, zoom)
  const [maxTileX, minTileY] = lonLatToTile(maxLon, maxLat, zoom)
  return (maxTileX - minTileX + 1) * (maxTileY - minTileY + 1)
}

function lonLatToTile(lon, lat, zoom) {
  const tileLimit = 2 ** zoom
  const clampedLon = Math.max(-180, Math.min(180, lon))
  const clampedLat = Math.max(-WEB_MERCATOR_MAX_LAT, Math.min(WEB_MERCATOR_MAX_LAT, lat))
  const x = Math.floor((clampedLon + 180) / 360 * tileLimit)
  const latitudeRadians = clampedLat * Math.PI / 180
  const y = Math.floor((1 - Math.asinh(Math.tan(latitudeRadians)) / Math.PI) / 2 * tileLimit)
  return [
    Math.max(0, Math.min(tileLimit - 1, x)),
    Math.max(0, Math.min(tileLimit - 1, y))
  ]
}
