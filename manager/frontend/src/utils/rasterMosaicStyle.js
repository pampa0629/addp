export const DEFAULT_RASTER_MOSAIC_GAMMA = 0.6

export function normalizeRasterMosaicGamma(value, fallback = DEFAULT_RASTER_MOSAIC_GAMMA) {
  const parsed = Number(value)
  if (Number.isFinite(parsed) && parsed > 0) return parsed
  return fallback
}

export function rasterMosaicTileURLWithStyle(template, style = {}) {
  const raw = String(template || '').trim()
  if (!raw) return ''
  const questionIndex = raw.indexOf('?')
  const path = questionIndex >= 0 ? raw.slice(0, questionIndex) : raw
  const query = questionIndex >= 0 ? raw.slice(questionIndex + 1) : ''
  const params = new URLSearchParams(query)

  const gamma = normalizeRasterMosaicGamma(style.gamma)
  params.set('gamma', formatStyleNumber(gamma))

  const displayMin = Number(style.displayMin)
  const displayMax = Number(style.displayMax)
  if (Number.isFinite(displayMin) && Number.isFinite(displayMax) && displayMax > displayMin) {
    params.set('display_min', formatStyleNumber(displayMin))
    params.set('display_max', formatStyleNumber(displayMax))
  } else {
    params.delete('display_min')
    params.delete('display_max')
  }

  if (style.invert) {
    params.set('invert', 'true')
  } else {
    params.delete('invert')
  }

  const nextQuery = params.toString()
  return nextQuery ? `${path}?${nextQuery}` : path
}

function formatStyleNumber(value) {
  return Number(value).toFixed(4).replace(/0+$/, '').replace(/\.$/, '')
}
