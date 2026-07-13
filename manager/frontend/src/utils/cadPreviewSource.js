function normalized(value) {
  return String(value || '').trim().toLowerCase()
}

export function isCADPreviewSource(previewData = {}, node = {}) {
  const object = previewData?.object || {}
  const item = object?.attributes?.item || {}
  const contentMetadata = object?.content?.metadata || {}
  const metadata = previewData?.metadata || {}
  const dataType = normalized(item.data_type || metadata.data_type || contentMetadata.data_type)
  const format = normalized(
    item.format ||
    metadata.source_format ||
    metadata.format ||
    contentMetadata.source_format ||
    contentMetadata.format ||
    node.format ||
    node.file_format
  )
  const layout = normalized(item.layout || metadata.layout || contentMetadata.layout)
  const path = String(
    node.path ||
    node.full_name ||
    node.table ||
    node.name ||
    node.label ||
    node.id ||
    ''
  ).trim()

  if (dataType === 'cad' && ['dwg', 'dxf'].includes(format) && (!layout || layout === 'single')) return true
  return !dataType && (!format || ['dwg', 'dxf'].includes(format)) && /\.(dwg|dxf)$/i.test(path)
}
