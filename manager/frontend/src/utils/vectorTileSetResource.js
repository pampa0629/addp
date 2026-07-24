const normalized = (value) => String(value || '').trim().toLowerCase()

const hasItemIdentity = (node) => {
  const itemID = Number(node?.metadata?.item_id || 0)
  if (Number.isFinite(itemID) && itemID > 0) return true
  return /(?:[?&])item_id=\d+(?:&|$)/.test(String(node?.locator || node?.id || ''))
}

const hasSpatialFacts = (node) => {
  const spatial = node?.metadata?.spatial
  if (spatial === true) return true
  return Boolean(spatial && typeof spatial === 'object' && Object.keys(spatial).length > 0)
}

export const isVectorTileSourceItem = (node) => {
  return hasItemIdentity(node) &&
    normalized(node?.metadata?.data_type) === 'table' &&
    hasSpatialFacts(node)
}

export const isVectorTilePreviewTarget = (target) => {
  const geometryColumns = Array.isArray(target?.geometryColumns) ? target.geometryColumns : []
  const hasGeometryColumn = Boolean(String(target?.geometryColumn || '').trim()) ||
    geometryColumns.some((column) => Boolean(String(column || '').trim()))

  return Boolean(String(target?.locator || '').trim()) &&
    Number(target?.itemID || 0) > 0 &&
    normalized(target?.locatorType) === 'table' &&
    hasGeometryColumn
}
