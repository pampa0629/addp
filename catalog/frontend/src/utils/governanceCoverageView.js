const dimensionFields = new Set(['name', 'description'])

export function coverageDimensionLabel(translate, dimensionKey, field, emptyLabel = '-') {
  if (typeof dimensionKey !== 'string' || dimensionKey.length === 0 || !dimensionFields.has(field)) {
    return emptyLabel
  }
  return translate(`catalog.coverage.dimensions.${dimensionKey}.${field}`)
}
