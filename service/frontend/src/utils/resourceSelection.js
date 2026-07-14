export const QUERY_TABLE_ENGINE_TYPES = ['postgresql', 'mysql', 'doris', 'clickhouse', 'minio', 's3']
export const NATIVE_TABLE_ENGINE_TYPES = ['postgresql', 'mysql', 'doris', 'clickhouse']
export const OBJECT_TABLE_FORMATS = ['parquet', 'orc', 'avro']

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
