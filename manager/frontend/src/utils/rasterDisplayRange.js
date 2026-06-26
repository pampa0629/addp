export function isRasterNoDataSample(value, noDataValue) {
  if (!Number.isFinite(value)) return true
  return Number.isFinite(noDataValue) && value === noDataValue
}

export function rasterPercentile(values, ratio) {
  if (!values.length) return 0
  const index = Math.max(0, Math.min(values.length - 1, Math.floor((values.length - 1) * ratio)))
  return values[index]
}

export function rasterDisplayRangeFromSamples(samples, noDataValue) {
  const values = []
  for (const sample of samples || []) {
    const value = Number(sample)
    if (isRasterNoDataSample(value, noDataValue)) continue
    values.push(value)
  }
  if (!values.length) return null
  values.sort((a, b) => a - b)
  let min = rasterPercentile(values, 0.02)
  let max = rasterPercentile(values, 0.98)
  if (max <= min) {
    min = values[0]
    max = values[values.length - 1]
  }
  if (!Number.isFinite(min) || !Number.isFinite(max) || max <= min) return null
  return {
    min,
    max,
    nodata: Number.isFinite(noDataValue) ? noDataValue : undefined
  }
}

export function rasterDisplayRangeFromMeta(rasterInfo) {
  if (!rasterInfo || typeof rasterInfo !== 'object') return null
  const min = Number(rasterInfo.display_min ?? rasterInfo.sample_min)
  const max = Number(rasterInfo.display_max ?? rasterInfo.sample_max)
  if (!Number.isFinite(min) || !Number.isFinite(max) || max <= min) return null
  const nodata = Number(rasterInfo.nodata)
  return {
    min,
    max,
    nodata: Number.isFinite(nodata) ? nodata : undefined
  }
}

export function rasterDisplayRangeFromGDALMetadata(metadata, noDataValue) {
  if (!metadata || typeof metadata !== 'object') return null
  const min = Number(metadata.STATISTICS_MINIMUM)
  const max = Number(metadata.STATISTICS_MAXIMUM)
  if (!Number.isFinite(min) || !Number.isFinite(max) || max <= min) return null
  return {
    min,
    max,
    nodata: Number.isFinite(noDataValue) ? noDataValue : undefined
  }
}

export function rasterSampleSize(width, height, maxPixels = 65536) {
  const sourceWidth = Math.max(1, Number(width) || 1)
  const sourceHeight = Math.max(1, Number(height) || 1)
  const sourcePixels = sourceWidth * sourceHeight
  if (sourcePixels <= maxPixels) {
    return { width: sourceWidth, height: sourceHeight }
  }
  const ratio = Math.sqrt(maxPixels / sourcePixels)
  return {
    width: Math.max(1, Math.round(sourceWidth * ratio)),
    height: Math.max(1, Math.round(sourceHeight * ratio))
  }
}
