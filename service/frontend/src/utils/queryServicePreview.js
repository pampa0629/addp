const positiveNumber = (value, fallback = 0) => {
  const number = Number(value)
  return Number.isFinite(number) && number > 0 ? number : fallback
}

const primaryGeometry = (spatial) => {
  const columns = Array.isArray(spatial?.geometry_columns) ? spatial.geometry_columns : []
  const primaryName = String(spatial?.primary_geometry_column || '').trim()
  return columns.find((column) => column?.name === primaryName) || (columns.length === 1 ? columns[0] : null)
}

export function buildQueryServicePreview({ rows, pagination, spatial } = {}) {
  const normalizedRows = Array.isArray(rows) ? rows : []
  const geometry = primaryGeometry(spatial)
  const geometryColumn = String(geometry?.name || '').trim()
  const sourceSRID = positiveNumber(geometry?.srid ?? spatial?.srid)
  const sourceCRS = String(geometry?.crs_ref || spatial?.crs_ref || (sourceSRID > 0 ? `EPSG:${sourceSRID}` : '')).trim()
  const definitions = Array.isArray(spatial?.crs_definitions) ? spatial.crs_definitions : []
  const sourceCRSDefinition = definitions.find((definition) => definition?.id === sourceCRS) || null

  return {
    columns: normalizedRows.length > 0 ? Object.keys(normalizedRows[0]) : [],
    rows: normalizedRows,
    total: positiveNumber(pagination?.total),
    page: positiveNumber(pagination?.page, 1),
    page_size: positiveNumber(pagination?.page_size, 20),
    geometry_columns: geometryColumn ? [geometryColumn] : [],
    source_srid: sourceSRID,
    source_crs: sourceCRS,
    source_crs_definition: sourceCRSDefinition,
    transform_status: sourceCRS ? 'not_transformed' : 'unknown_crs'
  }
}

export function queryServicePreviewFields({ configType, defaultFields, spatial } = {}) {
  if (configType !== 'table') return ''

  const fields = Array.isArray(defaultFields)
    ? defaultFields.map((field) => String(field || '').trim()).filter(Boolean)
    : []
  if (fields.length === 0) return ''

  const geometryColumn = String(primaryGeometry(spatial)?.name || '').trim()

  if (geometryColumn) fields.push(geometryColumn)
  return [...new Set(fields)].join(',')
}
