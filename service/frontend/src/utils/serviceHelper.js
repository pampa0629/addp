/**
 * Service 模块工具函数
 * 用于处理查询服务的协议配置、几何信息等
 */

/**
 * 检查查询服务协议是否启用
 * @param {Object} service - 查询服务对象
 * @param {String} protocolName - 协议名称 ('rest_api' | 'ogc_features')
 * @returns {Boolean} 协议是否启用
 */
export function isProtocolEnabled(service, protocolName) {
  if (!service || !service.protocols) return false
  return service.protocols[protocolName]?.enabled || false
}

/**
 * 获取查询服务支持的协议列表
 * @param {Object} service - 查询服务对象
 * @returns {Array} 协议列表 [{name, key}, ...]
 */
export function getEnabledProtocols(service) {
  const protocols = []
  if (isProtocolEnabled(service, 'rest_api')) {
    protocols.push({ name: 'REST API', key: 'rest_api' })
  }
  if (isProtocolEnabled(service, 'ogc_features')) {
    protocols.push({ name: 'OGC Features', key: 'ogc_features' })
  }
  return protocols
}

/**
 * 获取查询服务端点 URL
 * @param {Object} service - 查询服务对象
 * @param {String} baseURL - 基础 URL (默认为当前域名)
 * @returns {Object} 端点对象 {rest_api, ogc_features}
 */
export function getQueryServiceEndpoints(service, baseURL = '') {
  if (!service) return {}

  const endpoints = {}
  if (isProtocolEnabled(service, 'rest_api')) {
    endpoints.rest_api = `${baseURL}/api/query/${service.service_name}`
  }
  if (isProtocolEnabled(service, 'ogc_features')) {
    endpoints.ogc_features = `${baseURL}/ogc/features/${service.service_name}`
  }
  return endpoints
}

/**
 * 检查查询服务是否有几何字段
 * @param {Object} service - 查询服务对象
 * @returns {Boolean} 是否有几何字段
 */
export function hasGeometry(service) {
	return !!getGeometryInfo(service)?.column
}

/**
 * 获取几何字段信息
 * @param {Object} service - 查询服务对象
 * @returns {Object|null} 几何信息 {column, srid, types, extent}
 */
export function getGeometryInfo(service) {
	const spatial = service?.data_config?.source_snapshot?.spatial
	const columns = spatial?.geometry_columns || []
	const primaryName = spatial?.primary_geometry_column
	const geom = columns.find(column => column.name === primaryName) || (columns.length === 1 ? columns[0] : null)
	if (!geom?.name) return null
  return {
	column: geom.name,
	srid: geom.srid || spatial.srid || 0,
	types: geom.geometry_type ? [geom.geometry_type] : [],
	extent: spatial.extent || null,
	crs_ref: geom.crs_ref || spatial.crs_ref || ''
  }
}

/**
 * 获取服务配置类型的显示文本
 * @param {String} configType - 配置类型 ('table' | 'sql')
 * @returns {String} 显示文本
 */
export function getConfigTypeText(configType) {
  const typeMap = {
    table: '表格模式',
    sql: 'SQL 模式'
  }
  return typeMap[configType] || configType
}

/**
 * 获取协议配置的显示文本
 * @param {String} protocolKey - 协议 key
 * @returns {String} 显示文本
 */
export function getProtocolText(protocolKey) {
  const protocolMap = {
    rest_api: 'REST API',
    ogc_features: 'OGC API Features'
  }
  return protocolMap[protocolKey] || protocolKey
}

/**
 * 格式化查询服务的端点列表（用于详情页展示）
 * @param {Object} service - 查询服务对象
 * @param {String} baseURL - 基础 URL
 * @returns {Array} 端点列表 [{protocol, name, url, description}, ...]
 */
export function formatServiceEndpoints(service, baseURL = '') {
  const endpoints = []

  if (isProtocolEnabled(service, 'rest_api')) {
    endpoints.push({
      protocol: 'rest_api',
      name: 'REST API',
      url: `${baseURL}/api/query/${service.service_name}`,
      description: '支持查询参数: filter, fields, orderBy, page, pageSize, format'
    })
  }

  if (isProtocolEnabled(service, 'ogc_features')) {
    endpoints.push({
      protocol: 'ogc_features',
      name: 'OGC API Features',
      url: `${baseURL}/ogc/features/${service.service_name}`,
      description: 'OGC API - Features Part 1: Core 1.0 标准'
    })
  }

  return endpoints
}

/**
 * 验证服务名称格式（英数字下划线）
 * @param {String} serviceName - 服务名称
 * @returns {Boolean} 是否有效
 */
export function validateServiceName(serviceName) {
  if (!serviceName) return false
  const regex = /^[a-zA-Z0-9_]+$/
  return regex.test(serviceName)
}

/**
 * 获取服务状态的显示样式
 * @param {String} status - 状态 ('active' | 'inactive' | 'error')
 * @returns {Object} {type, text}
 */
export function getStatusDisplay(status) {
  const statusMap = {
    active: { type: 'success', text: '活跃' },
    inactive: { type: 'info', text: '未激活' },
    error: { type: 'danger', text: '错误' }
  }
  return statusMap[status] || { type: 'info', text: status }
}

/**
 * 复制文本到剪贴板（兼容旧浏览器）
 * @param {String} text - 要复制的文本
 * @returns {Promise<Boolean>} 复制是否成功
 */
export async function copyToClipboard(text) {
  try {
    // 优先使用现代 Clipboard API
    if (navigator.clipboard && navigator.clipboard.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }

    // 后备方案：使用传统的 execCommand 方法
    const textarea = document.createElement('textarea')
    textarea.value = text
    textarea.style.position = 'fixed'
    textarea.style.top = '0'
    textarea.style.left = '0'
    textarea.style.opacity = '0'
    document.body.appendChild(textarea)
    textarea.select()

    try {
      const successful = document.execCommand('copy')
      return successful
    } finally {
      document.body.removeChild(textarea)
    }
  } catch (error) {
    console.error('复制失败:', error)
    return false
  }
}
