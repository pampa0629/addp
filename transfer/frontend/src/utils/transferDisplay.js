function parseCapabilities(engine) {
  const raw = engine?.capabilities
  if (!raw) return null
  if (typeof raw === 'object') return raw
  if (typeof raw !== 'string') return null
  try {
    return JSON.parse(raw)
  } catch (_error) {
    return null
  }
}

function normalizeType(engineOrType) {
  const value = typeof engineOrType === 'string' ? engineOrType : engineOrType?.engine_type
  return String(value || '').toLowerCase()
}

export function hasStorageCapability(engine) {
  const caps = parseCapabilities(engine)
  if (caps) {
    const storage = caps.storage || {}
    const store = storage.store || {}
    return Boolean(
      storage.catalog?.supported ||
      storage.metadata?.supported ||
      Object.values(store).some(Boolean)
    )
  }
  return isNativeTableEngine(engine) || isContentEngine(engine) || isDocumentEngine(engine) || isGraphEngine(engine)
}

export function isNativeTableEngine(engineOrType) {
  const caps = typeof engineOrType === 'object' ? parseCapabilities(engineOrType) : null
  if (caps?.engine_family === 'tabular') return true
  const type = normalizeType(engineOrType)
  return ['postgres', 'mysql', 'doris', 'clickhouse', 'sqlite', 'spatialite', 'spark_sql'].some(token => type.includes(token))
}

export function isContentEngine(engineOrType) {
  const caps = typeof engineOrType === 'object' ? parseCapabilities(engineOrType) : null
  if (['file', 'object'].includes(caps?.engine_family)) return true
  const type = normalizeType(engineOrType)
  return ['nfs', 's3', 'minio', 'oss', 'objectstore', 'file'].some(token => type.includes(token))
}

export function isDocumentEngine(engineOrType) {
  const caps = typeof engineOrType === 'object' ? parseCapabilities(engineOrType) : null
  if (caps?.engine_family === 'document') return true
  return normalizeType(engineOrType).includes('mongo')
}

export function isGraphEngine(engineOrType) {
  const caps = typeof engineOrType === 'object' ? parseCapabilities(engineOrType) : null
  if (caps?.engine_family === 'graph') return true
  return normalizeType(engineOrType).includes('neo4j')
}

export function engineCategoryLabel(engineOrType) {
  if (isContentEngine(engineOrType)) return '文件/对象存储'
  if (isNativeTableEngine(engineOrType)) return '数据库表存储'
  if (isDocumentEngine(engineOrType)) return '文档集合存储'
  if (isGraphEngine(engineOrType)) return '图数据存储'
  return '存储引擎'
}

export function engineOptionLabel(engine) {
  const name = engine?.name || engine?.display_name || `#${engine?.id || ''}`
  return `${name}（${engineCategoryLabel(engine)}）`
}

export function catalogKindLabel(value) {
  const key = String(value || '').toLowerCase()
  const labels = {
    root: '根目录',
    server: '服务',
    database: '数据库',
    schema: 'Schema',
    namespace: '命名空间',
    table: '数据表',
    view: '视图',
    materialized_view: '物化视图',
    external_table: '外部表',
    bucket: '存储桶',
    prefix: '文件夹',
    directory: '文件夹',
    object: '文件',
    file: '文件',
    collection: '集合',
    label: '图节点标签',
    relationship: '图关系'
  }
  return labels[key] || value || '-'
}

export function dataTypeLabel(value) {
  const key = String(value || '').toLowerCase()
  const labels = {
    table: '表格数据',
    document: '文档数据',
    graph: '图数据',
    file: '文件',
    object: '对象'
  }
  return labels[key] || value || '-'
}

export function representationLabel(value) {
  const key = String(value || '').toLowerCase()
  const labels = {
    native: '引擎内原生数据',
    encoded: '文件中的数据'
  }
  return labels[key] || value || '-'
}

export function formatLabel(value) {
  const key = String(value || '').toLowerCase()
  const labels = {
    csv: 'CSV',
    tsv: 'TSV',
    json: 'JSON',
    jsonl: 'JSON Lines',
    parquet: 'Parquet',
    geojson: 'GeoJSON',
    shapefile: 'Shapefile'
  }
  return labels[key] || value || '-'
}

export function writeModeLabel(value) {
  const key = String(value || '').toLowerCase()
  const labels = {
    create_if_not_exists: '没有表就创建',
    append: '追加到已有数据后面',
    truncate_insert: '先清空再写入',
    overwrite: '覆盖原文件'
  }
  return labels[key] || value || '-'
}

export function writeModeDescription(value) {
  const key = String(value || '').toLowerCase()
  const descriptions = {
    create_if_not_exists: '目标表不存在时自动创建；如果表已存在，就按字段写入，不主动清空旧数据。',
    append: '保留目标表已有数据，把本次传输结果继续追加进去。',
    truncate_insert: '先清空目标表已有数据，再写入本次传输结果，适合全量刷新。',
    overwrite: '目标文件已存在时先删除旧文件，再写入新文件。'
  }
  return descriptions[key] || ''
}
