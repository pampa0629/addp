/**
 * TaskDetail 配置构建工具函数
 * 将复杂的配置转换逻辑提取出来,减少组件复杂度
 */

// ============ 基础工具函数 ============

export const toLower = (value) => (value || '').toString().toLowerCase()

export const toNumber = (value) => {
  if (value === undefined || value === null || value === '') {
    return null
  }
  const num = Number(value)
  return Number.isFinite(num) ? num : null
}

export const cloneDeep = (value) => {
  if (value === null || typeof value !== 'object') {
    return value
  }
  try {
    return JSON.parse(JSON.stringify(value))
  } catch {
    return value
  }
}

export const isS3Type = (resourceType) => {
  const type = toLower(resourceType)
  return type === 's3' || type === 'minio' || type === 'oss'
}

export const valueToStringArray = (value) => {
  if (Array.isArray(value)) {
    return value
      .map((item) => {
        if (item === undefined || item === null) return ''
        return String(item).trim()
      })
      .filter(Boolean)
  }
  if (value === undefined || value === null) {
    return []
  }
  const str = String(value).trim()
  return str ? [str] : []
}

// ============ 连接器配置转换 ============

export const resourceToConnectorForWorker = (resource) => {
  if (!resource) return {}
  const type = toLower(resource.resource_type)
  const conn = resource.connection_info || {}
  const connector = {}

  if (type === 'postgresql' || type === 'mysql') {
    connector.type = 'jdbc'
    connector.driver = type
    if (conn.host) connector.host = conn.host
    const port = toNumber(conn.port)
    if (port !== null) connector.port = port
    const username = conn.username || conn.user
    if (username) connector.username = username
    if (conn.password) connector.password = conn.password
    if (conn.database) connector.database = conn.database
    const sslMode = conn.ssl_mode || conn.sslmode
    if (sslMode) connector.ssl_mode = sslMode
  } else if (isS3Type(type)) {
    connector.type = 's3'
    if (conn.endpoint) connector.endpoint = conn.endpoint
    if (conn.access_key) connector.access_key = conn.access_key
    if (conn.secret_key) connector.secret_key = conn.secret_key
    if (conn.bucket) connector.bucket = conn.bucket
    if (conn.region) connector.region = conn.region
    connector.use_ssl = conn.use_ssl !== undefined ? !!conn.use_ssl : false
  }

  return connector
}

export const mergeTaskConnectorConfig = (base, extra, resourceType) => {
  const skipKeys = new Set([
    'host',
    'port',
    'user',
    'username',
    'password',
    'database',
    'driver',
    'endpoint',
    'access_key',
    'secret_key',
    'bucket',
    'connection_info',
    'scope',
    'system_engine_id',
    'local_engine_id',
    'local_resource_name',
    'resource_type'
  ])

  const isS3 = isS3Type(resourceType)

  Object.entries(extra || {}).forEach(([key, value]) => {
    if (value === undefined || value === null || value === '') {
      return
    }
    if (skipKeys.has(key)) {
      return
    }
    if (isS3 && key === 'path') {
      base.file_name = value
      return
    }
    if (isS3 && key === 'format') {
      base.file_type = value
      return
    }
    if (typeof value === 'string' && ['parameters', 'include_patterns', 'exclude_patterns', 'conflict_keys', 'geometry_fields'].includes(key)) {
      try {
        base[key] = JSON.parse(value)
        return
      } catch {
        // fall back to string
      }
    }
    base[key] = value
  })

  return base
}

export const sanitizeConnectorForWorker = (connector) => {
  const sanitized = {}
  Object.entries(connector || {}).forEach(([key, value]) => {
    if (value === undefined) {
      return
    }
    if (['scope', 'system_engine_id', 'local_engine_id', 'local_resource_name', 'resource_type'].includes(key)) {
      return
    }
    sanitized[key] = value
  })

  const type = toLower(sanitized.type || sanitized.driver)
  if (type === 'jdbc' || type === 'postgresql' || type === 'mysql') {
    if (!sanitized.type) sanitized.type = 'jdbc'
    if (!sanitized.driver && type !== 'jdbc') sanitized.driver = type
    if (!sanitized.username && sanitized.user) {
      sanitized.username = sanitized.user
    }
    delete sanitized.user
    if (sanitized.port !== undefined) {
      const port = toNumber(sanitized.port)
      if (port !== null) {
        sanitized.port = port
      } else {
        delete sanitized.port
      }
    }
    if (sanitized.sslmode && !sanitized.ssl_mode) {
      sanitized.ssl_mode = sanitized.sslmode
    }
    delete sanitized.sslmode
    if (sanitized.parameters && typeof sanitized.parameters === 'string') {
      try {
        sanitized.parameters = JSON.parse(sanitized.parameters)
      } catch {}
    }
  }

  if (type === 's3' || isS3Type(type)) {
    sanitized.type = 's3'
    sanitized.use_ssl = sanitized.use_ssl !== undefined ? !!sanitized.use_ssl : false
    if (sanitized.path && !sanitized.file_name) {
      sanitized.file_name = sanitized.path
    }
  }

  return sanitized
}

// ============ 字段映射转换 ============

export const extractGeometryFieldsForWorker = (config) => {
  if (!config || typeof config !== 'object') {
    return []
  }
  if (config.geometry_fields !== undefined) {
    return valueToStringArray(config.geometry_fields)
  }
  if (config.geometry_field !== undefined) {
    return valueToStringArray(config.geometry_field)
  }
  return []
}

export const normalizeSpatialFormatForWorker = (config) => {
  if (!config || typeof config !== 'object') {
    return ''
  }
  const preferred = toLower(config.spatial_format)
  if (preferred) return preferred
  const fileType = toLower(config.file_type || config.format)
  switch (fileType) {
    case 'geojson':
      return 'geojson'
    case 'csv-wkt':
      return 'wkt'
    case 'shapefile':
      return 'wkb'
    case 'ewkb':
    case 'ewkt':
    case 'hexwkb':
    case 'wkb':
    case 'wkt':
      return fileType
    default:
      return ''
  }
}

export const ensureGeometryMappingForWorker = (fieldMappings, geometryFields) => {
  if (!Array.isArray(fieldMappings) || !Array.isArray(geometryFields)) {
    return
  }
  const existing = new Set(
    fieldMappings
      .map((item) => (item?.target ? String(item.target).toLowerCase().trim() : ''))
      .filter(Boolean)
  )
  geometryFields.forEach((field) => {
    const name = String(field || '').trim()
    if (!name) return
    const lower = name.toLowerCase()
    if (existing.has(lower)) return
    fieldMappings.push({
      source: name,
      target: name,
      type: '',
      format: '',
      default_value: ''
    })
    existing.add(lower)
  })
}

export const buildFieldMappingsForWorker = (rawMappings, sourceConfigResolved, targetConfigResolved) => {
  if (!Array.isArray(rawMappings) || rawMappings.length === 0) {
    return []
  }
  const mapped = rawMappings.map((item) => ({
    source: item.source_field || '',
    target: item.target_field || '',
    type: item.field_type || '',
    format: item.format || '',
    default_value: item.default_value === undefined ? '' : item.default_value,
    nullable: item.nullable !== undefined ? !!item.nullable : true,
    transform: item.transform || ''
  }))

  const geometryFromTarget = extractGeometryFieldsForWorker(targetConfigResolved)
  const geometryFromSource = geometryFromTarget.length > 0
    ? geometryFromTarget
    : extractGeometryFieldsForWorker(sourceConfigResolved)

  if (geometryFromSource.length > 0) {
    ensureGeometryMappingForWorker(mapped, geometryFromSource)
  }

  return mapped
}

// ============ 类型推断 ============

export const inferConnectorTypeForWorker = (config) => {
  if (!config || typeof config !== 'object') {
    return 'unknown'
  }
  const explicit = toLower(config.type)
  if (explicit) return explicit
  if (config.driver) return 'jdbc'
  if (config.bucket || config.endpoint || config.file_name || config.file_type) return 's3'
  if (config.path || config.directory || config.file_path) return 'file'
  if (config.topic) return 'kafka'
  return 'unknown'
}

// ============ Worker 配置构建器 ============

export const buildWorkerConfigFromTask = (task, mappings, systemResourceMap) => {
  if (!task || !task.id) {
    return {}
  }

  const baseConfig = task.config || {}

  // 解析源配置
  const resolveConnectorConfig = (key) => {
    const rawConfig = cloneDeep(baseConfig[key] || {})
    const fallbackId = key === 'source' ? toNumber(task.source_id) : toNumber(task.target_id)

    let resource = null
    const embeddedId = toNumber(rawConfig.system_engine_id || rawConfig.engine_id)
    if (embeddedId !== null) {
      resource = systemResourceMap.get(embeddedId) || null
    }
    if (!resource && fallbackId !== null) {
      resource = systemResourceMap.get(fallbackId) || null
    }

    if (resource) {
      const connector = resourceToConnectorForWorker(resource)
      mergeTaskConnectorConfig(connector, rawConfig, resource.resource_type)
      return sanitizeConnectorForWorker(connector)
    }

    return sanitizeConnectorForWorker(rawConfig)
  }

  const sourceConfigResolved = resolveConnectorConfig('source')
  const targetConfigResolved = resolveConnectorConfig('target')

  const fieldMappingsForWorker = buildFieldMappingsForWorker(
    mappings,
    sourceConfigResolved,
    targetConfigResolved
  )

  const transformsList = []
  if (fieldMappingsForWorker.length > 0) {
    transformsList.push({
      type: 'field_mapping',
      name: 'FieldMapping',
      mappings: fieldMappingsForWorker
    })
  }

  const additionalTransforms = Array.isArray(baseConfig.transforms)
    ? cloneDeep(baseConfig.transforms)
    : []
  additionalTransforms.forEach((transform) => {
    if (transform && typeof transform === 'object') {
      transformsList.push(transform)
    }
  })

  const targetType = inferConnectorTypeForWorker(targetConfigResolved)
  const geometryFields = extractGeometryFieldsForWorker(targetConfigResolved)
  const fallbackGeometry = geometryFields.length > 0
    ? geometryFields
    : extractGeometryFieldsForWorker(sourceConfigResolved)

  const hasSpatialTransformConfig = transformsList.some((item) => {
    if (!item || typeof item !== 'object') return false
    const type = toLower(item.type || item.name)
    return type === 'spatial' || type === 'spatialtransform'
  })

  if (
    targetType === 's3' &&
    fallbackGeometry.length > 0 &&
    !hasSpatialTransformConfig
  ) {
    const spatialTransform = {
      type: 'spatial',
      geometry_fields: fallbackGeometry,
      source_format: 'wkb'
    }
    const targetFormat = normalizeSpatialFormatForWorker(targetConfigResolved)
    if (targetFormat) {
      spatialTransform.target_format = targetFormat
    }
    transformsList.push(spatialTransform)
  }

  const mapTaskModeForWorker = (mode) => {
    const normalized = toLower(mode)
    if (normalized === 'stream') return 'stream'
    if (normalized === 'micro-batch') return 'micro-batch'
    return 'batch'
  }

  return {
    task_id: task.id,
    name: task.name,
    description: task.description,
    source_id: task.source_id ?? null,
    target_id: task.target_id ?? null,
    schedule: task.schedule || '',
    mode: mapTaskModeForWorker(task.mode),
    batch_size: task.batch_size,
    max_parallelism: task.max_parallelism,
    retry_policy: task.retry_policy || null,
    source_config: {
      type: inferConnectorTypeForWorker(sourceConfigResolved),
      batch_size: task.batch_size,
      config: sourceConfigResolved
    },
    target_config: {
      type: inferConnectorTypeForWorker(targetConfigResolved),
      config: targetConfigResolved
    },
    transforms: transformsList
  }
}
