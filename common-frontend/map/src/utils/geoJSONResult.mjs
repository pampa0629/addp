function geometryValue(value) {
  if (!value) return null
  if (typeof value === 'string') {
    try { return geometryValue(JSON.parse(value)) } catch { return null }
  }
  if (value.type === 'Feature') return value.geometry || null
  if (typeof value.type === 'string' && (Array.isArray(value.coordinates) || value.type === 'GeometryCollection')) return value
  return null
}

export function spatialPreviewDescriptor(spatial, geometryField) {
  const field = (spatial?.geometry_fields || []).find((item) => item.name === geometryField)
  return {
    source_srid: field?.srid ?? spatial?.srid ?? 0,
    source_crs: field?.crs_ref || spatial?.crs_ref || ''
  }
}

export function validateGeoJSONResult(rows, hasMore = false) {
  if (hasMore) return { valid: false, reason: 'partial_result' }
  if (!Array.isArray(rows)) return { valid: false, reason: 'invalid_result' }
  if (rows.length > 1000) return { valid: false, reason: 'result_limit' }
  return { valid: true, reason: '' }
}

export function buildGeoJSONFeatures(rows, config, transformGeometry = (geometry) => geometry) {
  if (!Array.isArray(rows) || !config?.geometry_field) return []
  return rows.map((row, index) => {
    const geometry = transformGeometry(geometryValue(row?.[config.geometry_field]))
    if (!geometry) return null
    const propertyFields = new Set([
      ...(config.tooltip_fields || []),
      config.label_field,
      config.style?.field,
    ].filter(Boolean))
    const properties = Object.fromEntries([...propertyFields].map((field) => [field, row?.[field]]))
    return { type: 'Feature', id: String(index), geometry, properties }
  }).filter(Boolean)
}

export function resultSelectionFromFeature(feature, rowCount) {
  const rawID = feature?.id
  if (typeof rawID !== 'string' && typeof rawID !== 'number') return null
  const rowIndex = Number(rawID)
  return Number.isInteger(rowIndex) && rowIndex >= 0 && rowIndex < rowCount
    ? { row_index: rowIndex }
    : null
}
