const QUICK_VIEW_REASON_KEYS = {
  'spatial metadata is incomplete': 'manager.spatialPreview.spatialMetadataIncomplete',
  'quick view geometry metadata is unavailable': 'manager.spatialPreview.quickViewGeometryUnavailable',
  'quick view CRS is not renderable': 'manager.spatialPreview.quickViewCRSNotRenderable',
  'quick view row count is unavailable': 'manager.spatialPreview.quickViewRowCountUnavailable',
  'direct GeoJSON quick view exceeds row limit': 'manager.spatialPreview.directGeoJSONRowLimitExceeded',
  'tile generation requires geometry metadata': 'manager.spatialPreview.tileGenerationRequiresGeometry',
  'tile generation requires numeric SRID': 'manager.spatialPreview.tileGenerationRequiresNumericSRID',
  'tile generation requires spatial extent': 'manager.spatialPreview.tileGenerationRequiresSpatialExtent',
  'tile generation is unavailable': 'manager.spatialPreview.tileGenerationUnavailable'
}

export function quickViewReasonText(t, reason) {
  const normalized = String(reason || '').trim()
  const key = QUICK_VIEW_REASON_KEYS[normalized]
  return key ? t(key) : normalized
}
