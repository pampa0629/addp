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

export function rasterSpatialFacts(previewData, selectedNode) {
  const attrs = rasterMetaAttributes(previewData, selectedNode)
  return nestedObject(attrs, 'capabilities', 'spatial')
}
