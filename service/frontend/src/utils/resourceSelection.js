export const QUERY_TABLE_ENGINE_TYPES = ['postgresql', 'mysql', 'doris', 'clickhouse', 'minio', 's3']
export const NATIVE_TABLE_ENGINE_TYPES = ['postgresql', 'mysql', 'doris', 'clickhouse']
export const OBJECT_TABLE_FORMATS = ['parquet', 'orc', 'avro']
export const TILE_PYRAMID_ENGINE_TYPES = ['s3', 'minio', 'nfs']

export function isQueryableTableNode(node) {
  if (!node) return false
  if (String(node.type || '').toLowerCase() !== 'table') {
    return false
  }
  const metadata = node.metadata || {}
  const dataType = String(metadata.data_type || metadata.attributes?.item?.data_type || 'table').toLowerCase()
  const representation = String(metadata.representation || metadata.attributes?.item?.representation || '').toLowerCase()
  const format = String(metadata.format || metadata.attributes?.item?.format || '').toLowerCase()
  if (dataType !== 'table') {
    return false
  }
  if (!representation || representation === 'native') {
    return true
  }
  return representation === 'encoded' && OBJECT_TABLE_FORMATS.includes(format)
}

export function isQueryableTableVisibleNode(node) {
  if (!node) return false
  const type = String(node.type || '').toLowerCase()
  if (type === 'table') {
    return isQueryableTableNode(node)
  }
  return node.hasChildren || type === 'engine' || type === 'schema' || type === 'database' || type === 'bucket' || type === 'directory' || type === 'dir' || type === 'prefix' || type === 'root' || type === 'service' || type === 'server'
}

export function isNativeTableNode(node) {
  if (!node) return false
  if (String(node.type || '').toLowerCase() !== 'table') {
    return false
  }
  const metadata = node.metadata || {}
  const dataType = String(metadata.data_type || metadata.attributes?.item?.data_type || 'table').toLowerCase()
  const representation = String(metadata.representation || metadata.attributes?.item?.representation || 'native').toLowerCase()
  return dataType === 'table' && representation === 'native'
}

export function isNativeTableVisibleNode(node) {
  if (!node) return false
  const type = String(node.type || '').toLowerCase()
  if (type === 'table') {
    return isNativeTableNode(node)
  }
  return node.hasChildren || type === 'engine' || type === 'schema' || type === 'database' || type === 'bucket' || type === 'directory' || type === 'dir' || type === 'prefix' || type === 'root' || type === 'service' || type === 'server'
}

export function isTilePyramidNode(node) {
  if (!node) return false
  const type = String(node.type || '').toLowerCase()
  if (type !== 'object' && type !== 'file') return false
  const metadata = node.metadata || {}
  const dataType = String(metadata.data_type || metadata.attributes?.item?.data_type || '').toLowerCase()
  const format = String(metadata.format || metadata.attributes?.item?.format || '').toLowerCase()
  const layout = String(metadata.layout || metadata.attributes?.item?.layout || '').toLowerCase()
  return dataType === 'media' && format === 'tile_pyramid' && layout === 'whole'
}

export function isTilePyramidVisibleNode(node) {
  if (!node) return false
  if (isTilePyramidNode(node)) return true
  const type = String(node.type || '').toLowerCase()
  return node.hasChildren || ['engine', 'bucket', 'directory', 'dir', 'prefix', 'root', 'service'].includes(type)
}

export function defaultTileLayerName(label, itemId) {
  const normalized = String(label || '')
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '_')
    .replace(/^_+|_+$/g, '')
  if (!normalized) return `layer_${itemId}`
  return /^[0-9]/.test(normalized) ? `layer_${normalized}` : normalized
}
