const finiteNumbers = (value, length) => {
  if (!Array.isArray(value) || value.length < length) return null
  const numbers = value.slice(0, length).map(Number)
  return numbers.every(Number.isFinite) ? numbers : null
}

const clamp = (value, min, max) => Math.min(max, Math.max(min, value))

const validWGS84Extent = (value) => {
  const extent = finiteNumbers(value, 4)
  if (!extent) return null
  if (extent[0] < -180 || extent[2] > 180 || extent[1] < -90 || extent[3] > 90) return null
  return extent[0] <= extent[2] && extent[1] <= extent[3] ? extent : null
}

export function tilePreviewConfig(layer) {
  const snapshot = layer?.layer_config?.source_snapshot || {}
  const minZoom = clamp(Number.isFinite(Number(snapshot.min_zoom)) ? Number(snapshot.min_zoom) : 0, 0, 22)
  const maxZoom = clamp(Number.isFinite(Number(snapshot.max_zoom)) ? Number(snapshot.max_zoom) : 22, minZoom, 22)
  const snapshotCenter = finiteNumbers(snapshot.center, 3)
  const extent = validWGS84Extent(snapshot.spatial?.extent)
  const center = snapshotCenter
    ? snapshotCenter.slice(0, 2)
    : extent
      ? [(extent[0] + extent[2]) / 2, (extent[1] + extent[3]) / 2]
      : [0, 0]
  const zoom = clamp(snapshotCenter ? snapshotCenter[2] : minZoom, minZoom, maxZoom)

  return { center, extent, zoom, minZoom, maxZoom }
}

export function tilePreviewCoordinate(layer) {
  const { center, zoom } = tilePreviewConfig(layer)
  const z = Math.round(zoom)
  const scale = 2 ** z
  const longitude = clamp(center[0], -180, 180)
  const latitude = clamp(center[1], -85.05112878, 85.05112878)
  const latitudeRadians = latitude * Math.PI / 180
  return {
    z,
    x: Math.floor((longitude + 180) / 360 * scale),
    y: Math.floor((1 - Math.log(Math.tan(latitudeRadians) + 1 / Math.cos(latitudeRadians)) / Math.PI) / 2 * scale)
  }
}
