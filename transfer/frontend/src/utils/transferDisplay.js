import {
  engineCapabilityFamily,
  hasDeclaredStorageCapability,
  isContentStorageEngine,
  isNativeTableStorageEngine,
  parseEngineCapabilities
} from '@addp/common-frontend/basic/src/utils/engineCapabilities.mjs'

export function hasStorageCapability(engine) {
  return hasDeclaredStorageCapability(engine)
}

export function queryReadSessionCapability(engine) {
  const capabilities = parseEngineCapabilities(engine)
  const query = capabilities?.compute?.query
  if (query?.supported !== true || query?.read_session !== true) return null

  const languages = [...new Set((Array.isArray(query.languages) ? query.languages : [])
    .map(value => String(value || '').trim().toLowerCase())
    .filter(Boolean))]
  if (languages.length === 0) return null

  const declaredDefault = String(query.default_language || '').trim().toLowerCase()
  const identifierQuotes = query.identifier_quotes && typeof query.identifier_quotes === 'object' && !Array.isArray(query.identifier_quotes)
    ? Object.fromEntries(Object.entries(query.identifier_quotes).flatMap(([language, quote]) => {
      const normalizedLanguage = String(language || '').trim().toLowerCase()
      const normalizedQuote = String(quote || '')
      return languages.includes(normalizedLanguage) && Array.from(normalizedQuote).length === 1
        ? [[normalizedLanguage, normalizedQuote]]
        : []
    }))
    : {}
  const parameterLanguages = query.parameters?.supported === true
    ? new Set((query.parameters.languages || []).map(value => String(value || '').trim().toLowerCase()).filter(Boolean))
    : new Set()
  const parameterTypes = query.parameters?.supported === true
    ? new Set((query.parameters.types || []).map(value => String(value || '').trim().toLowerCase()).filter(Boolean))
    : new Set()

  return {
    engineFamily: String(capabilities?.engine_family || '').trim().toLowerCase(),
    languages,
    defaultLanguage: languages.includes(declaredDefault) ? declaredDefault : languages[0],
    identifierQuotes,
    parameterLanguages,
    parameterTypes
  }
}

export function hasIdempotentTableUpsert(engine) {
  const upsert = parseEngineCapabilities(engine)?.storage?.store?.table_upsert
  return upsert?.supported === true && upsert?.idempotent === true
}

export function hasNativeTableWriteCapability(engine) {
  const store = parseEngineCapabilities(engine)?.storage?.store
  return store?.table_write_prepare === true &&
    (store?.batch_write === true || store?.table_write_session === true)
}

export function hasContentWriteCapability(engine) {
  return parseEngineCapabilities(engine)?.storage?.store?.stream_write === true
}

export function hasBoundedWatermarkRead(engine) {
  return parseEngineCapabilities(engine)?.storage?.store?.bounded_watermark_read === true
}

export function hasAtomicPartitionedTableChangeApply(engine, requiredOperations = ['upsert']) {
  const apply = parseEngineCapabilities(engine)?.storage?.store?.partitioned_table_change_apply
  if (apply?.supported !== true || apply?.atomic_position_commit !== true || apply?.monotonic !== true) return false
  if (!Array.isArray(apply.position_types) || !apply.position_types.includes('kafka_offset/v1')) return false
  const operations = Array.isArray(apply.operations) ? apply.operations : []
  return requiredOperations.every(operation => operations.includes(operation))
}

export function isNativeTableEngine(engine) {
  return isNativeTableStorageEngine(engine)
}

export function isContentEngine(engine) {
  return isContentStorageEngine(engine)
}

export function isDocumentEngine(engine) {
  return hasStorageCapability(engine) && engineCapabilityFamily(engine) === 'document'
}

export function isGraphEngine(engine) {
  return hasStorageCapability(engine) && engineCapabilityFamily(engine) === 'graph'
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
    media: '媒体数据',
    unknown: '未知数据',
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
    shapefile: 'Shapefile',
    pdf: 'PDF',
    docx: 'DOCX',
    pptx: 'PPTX',
    wps: 'WPS',
    text: 'Text',
    markdown: 'Markdown',
    jpeg: 'JPEG',
    jpg: 'JPEG',
    png: 'PNG',
    gif: 'GIF',
    tiff: 'TIFF',
    webp: 'WebP',
    bmp: 'BMP',
    svg: 'SVG',
    avif: 'AVIF',
    heic: 'HEIC',
    mp4: 'MP4',
    mov: 'MOV',
    mkv: 'MKV',
    avi: 'AVI',
    webm: 'WebM',
    mp3: 'MP3',
    wav: 'WAV',
    flac: 'FLAC',
    aac: 'AAC',
    ogg: 'OGG',
    unknown: 'Unknown'
  }
  return labels[key] || value || '-'
}

export function writeModeLabel(value) {
  const key = String(value || '').toLowerCase()
  const labels = {
    append: '追加',
    overwrite: '覆盖'
  }
  return labels[key] || value || '-'
}

export function writeModeDescription(value) {
  const key = String(value || '').toLowerCase()
  const descriptions = {
    append: '目标不存在时自动创建；目标已存在时保留原有数据，把本次数据追加进去。',
    overwrite: '目标不存在时自动创建；目标已存在时先清空或覆盖，再写入本次数据。'
  }
  return descriptions[key] || ''
}
