/**
 * 地图要素格式化工具函数
 */

/**
 * 格式化要素属性为分层卡片式 HTML
 * @param {Object} properties - 要素属性对象
 * @param {string} geomColumn - 几何列名（默认'geom'）
 * @returns {string} 格式化后的HTML字符串
 */
export function formatFeatureProperties(properties, geomColumn = 'geom') {
  const geometryColumns = [geomColumn, 'geom', 'geometry', 'the_geom', 'wkb_geometry']

  // 提取要素ID
  const featureId = properties.id || properties.ID || properties._id || properties.uuid || '未知'

  // 获取几何类型（从 geometry 对象中提取）
  let geomType = '未知类型'
  if (properties.geometry && properties.geometry.type) {
    const typeMap = {
      'Point': '点',
      'MultiPoint': '多点',
      'LineString': '线',
      'MultiLineString': '多线',
      'Polygon': '面',
      'MultiPolygon': '多面'
    }
    geomType = typeMap[properties.geometry.type] || properties.geometry.type
  }

  // 识别主要字段（优先级：name > 名称 > 属性1 > 第一个非ID非几何字段）
  const primaryFieldCandidates = ['name', '名称', 'NAME', '属性1', 'title', 'label']
  let primaryField = null
  let primaryValue = null

  for (const candidate of primaryFieldCandidates) {
    if (properties[candidate] && properties[candidate] !== featureId) {
      primaryField = candidate
      primaryValue = properties[candidate]
      break
    }
  }

  // 如果没找到候选字段，使用第一个非ID非几何的字段
  if (!primaryValue) {
    for (const [key, value] of Object.entries(properties)) {
      if (!geometryColumns.includes(key) &&
          key !== 'geometry' &&
          key !== 'id' && key !== 'ID' && key !== '_id' && key !== 'uuid' &&
          value !== null && value !== undefined) {
        primaryField = key
        primaryValue = value
        break
      }
    }
  }

  // 构建HTML
  let html = '<div class="feature-card">'

  // 卡片头部
  html += '<div class="feature-card-header">'
  html += `<div class="feature-id"><span class="id-icon">📍</span> ID: ${featureId}</div>`
  html += `<div class="feature-geom-type">${geomType}</div>`
  html += '</div>'

  // 主要字段（大字号显示）
  if (primaryValue) {
    html += '<div class="feature-primary-field">'
    html += `<div class="primary-value">${primaryValue}</div>`
    html += `<div class="primary-label">${primaryField}</div>`
    html += '</div>'
  }

  // 属性列表
  html += '<div class="feature-attributes">'
  let attributeCount = 0
  for (const [key, value] of Object.entries(properties)) {
    // 跳过几何列、内部属性、已显示的主要字段
    if (geometryColumns.includes(key) ||
        key === 'geometry' ||
        key === primaryField ||
        key === 'id' || key === 'ID' || key === '_id' || key === 'uuid') {
      continue
    }

    // 格式化值
    let displayValue = value
    if (value === null || value === undefined) {
      displayValue = '<span class="null-value">NULL</span>'
    } else if (typeof value === 'object') {
      displayValue = JSON.stringify(value)
    } else if (typeof value === 'string' && value.length > 80) {
      displayValue = value.substring(0, 80) + '...'
    }

    html += '<div class="attribute-item" style="color:#333">'
    html += `<span class="attr-key" style="color:#555;font-weight:600">${key}:</span> `
    html += `<span class="attr-value" style="color:#111">${displayValue}</span>`
    html += '</div>'

    attributeCount++
    if (attributeCount >= 10) break // 最多显示10个属性
  }
  html += '</div>'

  html += '</div>'
  return html
}
