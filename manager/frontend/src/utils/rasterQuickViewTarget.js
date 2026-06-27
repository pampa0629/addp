const asPlainObject = (value) => {
  return value && typeof value === 'object' && !Array.isArray(value) ? value : {}
}

const nestedObject = (source, ...path) => {
  let current = asPlainObject(source)
  for (const segment of path) {
    current = asPlainObject(current[segment])
  }
  return current
}

const normalizedFormat = (value) => String(value || '').trim().toLowerCase()

export function rasterMetaAttributes(previewData, selectedNode) {
  const preview = asPlainObject(previewData)
  const objectAttrs = nestedObject(preview, 'object', 'attributes')
  if (Object.keys(objectAttrs).length > 0) {
    return objectAttrs
  }

  const nodeAttrs = nestedObject(selectedNode, 'attributes')
  if (Object.keys(nodeAttrs).length > 0) {
    return nodeAttrs
  }

  return asPlainObject(preview.attributes)
}

export function isTIFFRasterMeta(previewData, selectedNode) {
  const attrs = rasterMetaAttributes(previewData, selectedNode)
  const item = nestedObject(attrs, 'item')
  return normalizedFormat(item.data_type) === 'media' &&
    normalizedFormat(item.format) === 'tiff'
}

export function isRasterMosaicMeta(previewData, selectedNode) {
  const attrs = rasterMetaAttributes(previewData, selectedNode)
  const item = nestedObject(attrs, 'item')
  return normalizedFormat(item.data_type) === 'media' &&
    normalizedFormat(item.format) === 'raster_mosaic' &&
    normalizedFormat(item.layout) === 'whole'
}

export function rasterSpatialFacts(previewData, selectedNode) {
  const attrs = rasterMetaAttributes(previewData, selectedNode)
  return nestedObject(attrs, 'capabilities', 'spatial')
}

export function rasterExtentLooksGeographic(extent) {
  if (!Array.isArray(extent) || extent.length !== 4) return false
  const [minX, minY, maxX, maxY] = extent.map(Number)
  return [minX, minY, maxX, maxY].every(Number.isFinite) &&
    minX >= -180 && minX <= 180 &&
    maxX >= -180 && maxX <= 180 &&
    minY >= -90 && minY <= 90 &&
    maxY >= -90 && maxY <= 90 &&
    maxX > minX &&
    maxY > minY
}

export function rasterExtentSRIDFromMetadata(metadata, extent = metadata?.extent) {
  const srid = Number(metadata?.extent_srid || metadata?.srid || metadata?.source_srid || 0)
  if (Number.isFinite(srid) && srid > 0) return srid
  return rasterExtentLooksGeographic(extent) ? 4326 : 0
}
