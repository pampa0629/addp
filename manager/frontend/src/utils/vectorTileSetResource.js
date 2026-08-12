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

export const isDeferredExtentDatabaseSource = (selection) => {
  const engineType = normalized(selection?.display?.engine_type || selection?.raw?.engine?.engine_type)
  return engineType === 'mysql' || engineType === 'oracle'
}

export const hasRequiredVectorTileSpatialFacts = (facts, selection) => {
  if (!String(facts?.geometryColumn || '').trim() || Number(facts?.sourceSRID || 0) <= 0) return false
  if (isDeferredExtentDatabaseSource(selection)) return true
  return Number(facts?.extentSRID || 0) > 0 && Array.isArray(facts?.extent) && facts.extent.length === 4
}

export const resolveVectorTileZoomRecommendation = (zoom = {}, quickView = {}, hasExtent = false) => {
  const minZoom = Number(zoom.min_zoom ?? quickView.min_zoom ?? 0)
  const declaredMaxZoom = Number(zoom.max_zoom ?? quickView.max_zoom ?? 12)
  return {
    minZoom,
    maxZoom: hasExtent ? declaredMaxZoom : Math.max(minZoom, Math.min(declaredMaxZoom, 12))
  }
}
