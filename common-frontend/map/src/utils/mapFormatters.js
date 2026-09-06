/**
 * 地图要素属性格式化工具。
 */

import { fieldPresentationFor, fieldPresentationLabel, formatFieldPresentationValue } from '../../../basic/src/utils/fieldPresentation.mjs'

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

const formatDisplayValue = (value, labels, presentation, locale) => {
  if (value === null || value === undefined) {
    return `<span class="null-value">${escapeHTML(labels.nullValue)}</span>`
  }
  const raw = formatFieldPresentationValue(value, presentation, locale, labels.nullValue)
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
  const presentations = normalizedOptions.fieldPresentations || []
  const locale = normalizedOptions.locale || 'zh-CN'

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

  const configuredFields = Array.isArray(normalizedOptions.fields) ? normalizedOptions.fields.filter(Boolean) : []
  const attributeSource = configuredFields.length > 0
    ? configuredFields.map((key) => [key, properties[key]]).filter(([, value]) => value !== undefined)
    : Object.entries(properties)
  const attributeRows = attributeSource
    .filter(([key]) => !isSkippedProperty(key, { geometryColumns, primaryField }) || (configuredFields.length > 0 && ID_FIELDS.includes(key) && key !== primaryField))
    .slice(0, 12)
    .map(([key, value]) => (
      '<div class="attribute-item">' +
        `<span class="attr-key">${escapeHTML(fieldPresentationLabel(key, presentations))}:</span> ` +
        `<span class="attr-value">${formatDisplayValue(value, labels, fieldPresentationFor(key, presentations), locale)}</span>` +
      '</div>'
    ))
    .join('')

  const primaryHTML = primaryValue !== undefined && primaryValue !== null
    ? '<div class="feature-primary-field">' +
        `<div class="primary-value">${formatDisplayValue(primaryValue, labels, fieldPresentationFor(primaryField, presentations), locale)}</div>` +
        `<div class="primary-label">${escapeHTML(fieldPresentationLabel(primaryField, presentations))}</div>` +
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
