const cleanString = (value) => String(value || '').trim()

const positiveNumber = (value) => {
  const parsed = Number(value || 0)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0
}

const cleanList = (items) => {
  if (!Array.isArray(items)) return []
  return items.map((item) => cleanString(item)).filter(Boolean)
}

const cleanExtent = (extent) => Array.isArray(extent) && extent.length === 4 ? extent : []

const cleanQuery = (query) => Object.fromEntries(
  Object.entries(query).filter(([, value]) => value !== '' && value !== undefined && value !== null)
)

const withIdentityFields = (query, context) => ({
  ...query,
  ...(context.locator ? { locator: context.locator } : {})
})

function quickViewContext(target = {}, quickViewStatus = {}) {
  const quickView = quickViewStatus?.quick_view || {}
  const renderFacts = quickViewStatus?.render_facts || {}
  const quickViewGeometryColumns = cleanList(quickView.geometry_columns)
  return {
    engineId: positiveNumber(target.engineId),
    schema: cleanString(target.schema),
    table: cleanString(target.table),
    locator: cleanString(target.locator),
    itemID: positiveNumber(target.itemID),
    itemFingerprint: cleanString(quickViewStatus?.item_fingerprint || target.itemFingerprint),
    geometryColumn: cleanString(quickView.geometry_column || target.geometryColumn),
    geometryColumns: quickViewGeometryColumns.length ? quickViewGeometryColumns : cleanList(target.geometryColumns),
    sourceSRID: positiveNumber(renderFacts.source_srid || quickView.source_srid || target.sourceSRID),
    renderExtentSRID: positiveNumber(renderFacts.render_extent_srid || quickView.extent_srid || target.extentSRID),
    renderExtent: cleanExtent(renderFacts.render_extent).length
      ? cleanExtent(renderFacts.render_extent)
      : cleanExtent(quickView.extent).length
        ? cleanExtent(quickView.extent)
        : cleanExtent(target.extent)
  }
}

export function buildTileCacheCreateQuery(target = {}, quickViewStatus = {}) {
  const context = quickViewContext(target, quickViewStatus)
  return cleanQuery(withIdentityFields({
    tab: 'tasks',
    create: '1',
    ...(context.itemID ? { item_id: String(context.itemID) } : {}),
    ...(context.geometryColumn ? { geom: context.geometryColumn } : {}),
    ...(context.itemFingerprint ? { item_fingerprint: context.itemFingerprint } : {}),
    ...(context.geometryColumns.length ? { geometry_columns: context.geometryColumns.join(',') } : {}),
    ...(context.sourceSRID ? { source_srid: String(context.sourceSRID) } : {}),
    ...(context.renderExtentSRID ? { extent_srid: String(context.renderExtentSRID) } : {}),
    ...(context.renderExtent.length === 4 ? { extent: context.renderExtent.join(',') } : {})
  }, context))
}

export function buildQuickViewOptimizationCreateQuery(target = {}, quickViewStatus = {}) {
  const context = quickViewContext(target, quickViewStatus)
  return cleanQuery(withIdentityFields({
    tab: 'tasks',
    create: '1',
    ...(context.itemID ? { item_id: String(context.itemID) } : {}),
    ...(context.itemFingerprint ? { item_fingerprint: context.itemFingerprint } : {}),
    ...(context.geometryColumn ? { geom: context.geometryColumn } : {}),
    ...(context.geometryColumns.length ? { geometry_columns: context.geometryColumns.join(',') } : {}),
    ...(context.sourceSRID ? { source_srid: String(context.sourceSRID) } : {})
  }, context))
}
