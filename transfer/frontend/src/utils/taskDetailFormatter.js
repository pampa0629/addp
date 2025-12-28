/**
 * TaskDetail 显示格式化工具函数
 */

import { toLower } from './workerConfigBuilder'

// ============ 标签和文本映射 ============

export const connectorTypeLabels = {
  postgresql: 'PostgreSQL',
  mysql: 'MySQL',
  s3: '对象存储(S3 兼容)',
  minio: '对象存储(MinIO)',
  oss: '对象存储(OSS)',
  csv: 'CSV 文件',
  json: 'JSON 文件',
  spatialite: 'SpatiaLite',
  sqlite: 'SQLite'
}

export const incrementalTypeLabels = {
  timestamp: '时间戳',
  numeric: '数值',
  string: '字符串'
}

// ============ 格式化函数 ============

export const formatDate = (date) => {
  if (!date) return '-'
  try {
    const d = new Date(date)
    if (isNaN(d.getTime())) return '-'

    const year = d.getFullYear()
    const month = String(d.getMonth() + 1).padStart(2, '0')
    const day = String(d.getDate()).padStart(2, '0')
    const hours = String(d.getHours()).padStart(2, '0')
    const minutes = String(d.getMinutes()).padStart(2, '0')
    const seconds = String(d.getSeconds()).padStart(2, '0')

    return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`
  } catch (e) {
    return '-'
  }
}

export const formatScopeLabel = (scope) => {
  if (!scope) return ''
  if (scope === 'system') return '系统资源'
  if (scope === 'local') return '本地存储引擎'
  return scope
}

export const formatValue = (value) => {
  if (value === undefined || value === null) return '-'
  if (typeof value === 'boolean') return value ? '是' : '否'
  if (Array.isArray(value)) {
    const joined = value.filter(item => item !== undefined && item !== null && item !== '').join('、')
    return joined || '-'
  }
  if (typeof value === 'object') {
    try {
      const json = JSON.stringify(value)
      return json && json !== '{}' ? json : '-'
    } catch {
      return '-'
    }
  }
  const str = String(value)
  return str.trim() ? str : '-'
}

export const getConnectorTypeLabel = (type) => {
  const lower = toLower(type)
  return connectorTypeLabels[lower] || (type ? type.toUpperCase() : '')
}

// ============ 状态标签 ============

export const getTaskStatusLabel = (taskData) => {
  if (!taskData) return '未执行'
  if (!taskData.schedule) {
    const labels = {
      pending: '未执行',
      running: '执行中',
      stopped: '已停止',
      completed: '已完成'
    }
    return labels[taskData.status] || '未执行'
  }
  if (['scheduled', 'running'].includes(taskData.status)) return '已启动'
  if (['pending', 'paused'].includes(taskData.status)) return '未启动'
  if (taskData.status === 'stopped') return '已停止'
  return '未启动'
}

export const getTaskStatusTagType = (taskData) => {
  const label = getTaskStatusLabel(taskData)
  const types = {
    未执行: 'info',
    执行中: 'primary',
    已停止: 'info',
    已完成: 'success',
    已启动: 'primary',
    未启动: 'info'
  }
  return types[label] || ''
}

export const getLastExecutionLabel = (status) => {
  if (!status || status === 'pending') return '未执行'
  if (status === 'running') return '执行中'
  if (status === 'success') return '成功'
  if (status === 'failed' || status === 'cancelled') return '失败'
  return status
}

export const getExecutionTagType = (status) => {
  const label = getLastExecutionLabel(status)
  const types = {
    未执行: 'info',
    执行中: 'primary',
    成功: 'success',
    失败: 'danger'
  }
  return types[label] || 'info'
}

export const getExecutionLabel = (status) => {
  const label = getLastExecutionLabel(status)
  return label === '未执行' ? '待开始' : label
}

// ============ 配置详情构建 ============

export const inferScope = (config, fallbackId) => {
  const raw = toLower(config.scope)
  if (raw) return raw
  if (fallbackId !== undefined && fallbackId !== null) return 'system'
  if (config.system_engine_id !== undefined && config.system_engine_id !== null) return 'system'
  if (config.local_engine_id !== undefined && config.local_engine_id !== null) return 'local'
  if (config.connection_info) return 'local'
  return ''
}

const addItem = (items, label, value, span) => {
  const display = formatValue(value)
  if (display === '-') return
  items.push({ label, value: display, span })
}

export const buildConnectorDetails = (task, role, systemResourceMap) => {
  const config = task?.config?.[role] || {}
  const fallbackId = role === 'source' ? task?.source_id : task?.target_id
  const items = []

  const scope = inferScope(config, fallbackId)
  addItem(items, '范围', formatScopeLabel(scope))

  if (scope === 'system') {
    const resourceId = fallbackId ?? config.system_engine_id
    const resource = resourceId !== undefined && resourceId !== null
      ? systemResourceMap.get(Number(resourceId))
      : null
    const resourceName = resource?.name
    const resourceType = resource?.resource_type || config.resource_type
    const labelParts = []
    if (resourceName) labelParts.push(resourceName)
    if (resourceId !== undefined && resourceId !== null) labelParts.push(`ID: ${resourceId}`)
    addItem(items, '数据源', labelParts.length ? labelParts.join(' / ') : '未配置')
    addItem(items, '连接类型', getConnectorTypeLabel(resourceType))
  } else if (scope === 'local') {
    const resourceId = config.local_engine_id
    const resourceName = config.local_resource_name
    const labelParts = []
    if (resourceName) labelParts.push(resourceName)
    if (resourceId !== undefined && resourceId !== null) labelParts.push(`ID: ${resourceId}`)
    addItem(items, '数据源', labelParts.length ? labelParts.join(' / ') : '未配置')
    addItem(items, '连接类型', getConnectorTypeLabel(config.resource_type || config.type || config.driver))
  } else {
    addItem(items, '数据源', '未配置')
    addItem(items, '连接类型', getConnectorTypeLabel(config.resource_type || config.type || config.driver))
  }

  const queryType = toLower(config.queryType || config.query_type)
  if (config.table) {
    addItem(items, '表名', config.table)
  }
  if (config.query) {
    const label = queryType === 'sql' ? 'SQL 查询' : '查询'
    addItem(items, label, config.query, 2)
  }
  if (config.path) {
    addItem(items, '路径', config.path, 2)
  }

  if (config.prefix) {
    addItem(items, '前缀', config.prefix)
  }
  if (config.format) {
    addItem(items, '格式', config.format)
  }
  if (config.encoding) {
    addItem(items, '编码', config.encoding)
  }
  if (config.delimiter) {
    addItem(items, '分隔符', config.delimiter)
  }
  if (config.incremental_field) {
    addItem(items, '增量字段', config.incremental_field)
    addItem(items, '增量类型', incrementalTypeLabels[toLower(config.incremental_type)] || config.incremental_type)
  }
  if (config.include_patterns) {
    addItem(items, '包含模式', config.include_patterns)
  }
  if (config.exclude_patterns) {
    addItem(items, '排除模式', config.exclude_patterns)
  }
  if (config.geometry_fields || config.geometry_field) {
    const fields = Array.isArray(config.geometry_fields)
      ? config.geometry_fields
      : [config.geometry_field].filter(Boolean)
    addItem(items, '空间字段', fields)
  }
  if (config.spatial_format) {
    addItem(items, '空间格式', config.spatial_format)
  }
  if (config.has_header !== undefined) {
    addItem(items, '包含表头', !!config.has_header)
  }
  if (config.recursive !== undefined) {
    addItem(items, '递归遍历', !!config.recursive)
  }
  if (config.overwrite !== undefined) {
    addItem(items, '覆盖写入', !!config.overwrite)
  }

  const connection =
    config.connection_info ||
    (scope === 'system'
      ? (systemResourceMap.get(Number(config.system_engine_id ?? fallbackId))?.connection_info || {})
      : {})
  if (connection.host) {
    const host = connection.port ? `${connection.host}:${connection.port}` : connection.host
    addItem(items, '主机', host)
  }
  if (connection.database) {
    addItem(items, '数据库', connection.database)
  }
  if (connection.schema) {
    addItem(items, 'Schema', connection.schema)
  }
  if (connection.endpoint) {
    addItem(items, 'Endpoint', connection.endpoint)
  }
  if (connection.bucket) {
    addItem(items, '存储桶', connection.bucket)
  }
  if (connection.region) {
    addItem(items, '区域', connection.region)
  }
  if (connection.path) {
    addItem(items, '目录', connection.path)
  }

  return items
}
