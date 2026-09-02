/**
 * 地图要素属性格式化工具。
 */

const DEFAULT_LABELS = {
  id: 'ID',
  unknown: 'Unknown',
  unknownGeometry: 'Unknown geometry',
  nullValue: 'NULL',
  noAttributes: 'No attributes'
}

const GEOMETRY_TYPE_LABELS = {
  Point: 'Point',
  MultiPoint: 'MultiPoint',
  LineString: 'LineString',
  MultiLineString: 'MultiLineString',
  Polygon: 'Polygon',
  MultiPolygon: 'MultiPolygon'
}

const PRIMARY_FIELD_CANDIDATES = ['name', '名称', 'NAME', 'title', 'label', 'code']
const ID_FIELDS = ['id', 'ID', '_id', 'uuid']
const DEFAULT_GEOMETRY_COLUMNS = ['geom', 'geometry', 'the_geom', 'wkb_geometry']

const escapeHTML = (value) => String(value)
  .replaceAll('&', '&amp;')
  .replaceAll('<', '&lt;')
  .replaceAll('>', '&gt;')
  .replaceAll('"', '&quot;')
  .replaceAll("'", '&#39;')

const normalizeOptions = (options) => {
  if (typeof options === 'string') {
    return {
      geomColumn: options
    }
  }
  return options || {}
}

const normalizeGeometryColumns = (geomColumn) => {
  const geometryColumns = new Set(DEFAULT_GEOMETRY_COLUMNS)
  if (geomColumn) geometryColumns.add(geomColumn)
  return geometryColumns
}

const isSkippedProperty = (key, { geometryColumns, primaryField }) => {
  return geometryColumns.has(key) ||
    key === primaryField ||
    ID_FIELDS.includes(key) ||
    key === 'originalFeature' ||
    key === 'rowData'
}

const formatDisplayValue = (value, labels) => {
  if (value === null || value === undefined) {
    return `<span class="null-value">${escapeHTML(labels.nullValue)}</span>`
  }
  if (typeof value === 'object') {
    return escapeHTML(JSON.stringify(value))
  }
  const raw = String(value)
  const displayValue = raw.length > 120 ? `${raw.slice(0, 120)}...` : raw
  return escapeHTML(displayValue)
}

/**
 * 格式化要素属性为弹窗 HTML。
 * @param {Object} properties 要素属性对象。
 * @param {Object|string} options 格式化选项，或旧式几何列名。
 * @param {string} options.geomColumn 几何列名。
 * @param {Object} options.labels UI 标签。
 * @returns {string} 格式化后的 HTML 字符串。
 */
export function formatFeatureProperties(properties = {}, options = {}) {
  const normalizedOptions = normalizeOptions(options)
  const labels = {
    ...DEFAULT_LABELS,
    ...(normalizedOptions.labels || {})
  }
  const geometryTypeLabels = {
    ...GEOMETRY_TYPE_LABELS,
    ...(normalizedOptions.geometryTypeLabels || {})
  }
  const geometryColumns = normalizeGeometryColumns(normalizedOptions.geomColumn)

  const resolvedFeatureId = ID_FIELDS.map((field) => properties[field]).find((value) => value !== undefined && value !== null)
  const featureId = resolvedFeatureId ?? labels.unknown
  const geometryType = properties.geometry?.type
  const geometryTypeLabel = geometryTypeLabels[geometryType] || geometryType || labels.unknownGeometry

  let primaryField = ''
  let primaryValue

  const configuredPrimaryField = normalizedOptions.primaryField
  if (configuredPrimaryField && properties[configuredPrimaryField] !== undefined && properties[configuredPrimaryField] !== null) {
    primaryField = configuredPrimaryField
    primaryValue = properties[configuredPrimaryField]
  }

  for (const candidate of primaryValue === undefined ? PRIMARY_FIELD_CANDIDATES : []) {
    if (properties[candidate] !== undefined && properties[candidate] !== null && properties[candidate] !== featureId) {
      primaryField = candidate
      primaryValue = properties[candidate]
      break
    }
  }

  if (primaryValue === undefined || primaryValue === null) {
    for (const [key, value] of Object.entries(properties)) {
      if (!isSkippedProperty(key, { geometryColumns, primaryField }) && value !== null && value !== undefined) {
        primaryField = key
        primaryValue = value
        break
      }
    }
  }

  const attributeRows = Object.entries(properties)
    .filter(([key]) => !isSkippedProperty(key, { geometryColumns, primaryField }))
    .slice(0, 12)
    .map(([key, value]) => (
      '<div class="attribute-item">' +
        `<span class="attr-key">${escapeHTML(key)}:</span> ` +
        `<span class="attr-value">${formatDisplayValue(value, labels)}</span>` +
      '</div>'
    ))
    .join('')

  const primaryHTML = primaryValue !== undefined && primaryValue !== null
    ? '<div class="feature-primary-field">' +
        `<div class="primary-value">${formatDisplayValue(primaryValue, labels)}</div>` +
        `<div class="primary-label">${escapeHTML(primaryField)}</div>` +
      '</div>'
    : ''

  return '<div class="feature-card">' +
    '<div class="feature-card-header">' +
      `<div class="feature-id">${escapeHTML(labels.id)}: ${escapeHTML(featureId)}</div>` +
      `<div class="feature-geom-type">${escapeHTML(geometryTypeLabel)}</div>` +
    '</div>' +
    primaryHTML +
    '<div class="feature-attributes">' +
      (attributeRows || `<div class="attribute-empty">${escapeHTML(labels.noAttributes)}</div>`) +
    '</div>' +
  '</div>'
}
