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

export function objectTableConfigFromSelection(selection) {
  const metadata = selection?.raw?.node?.metadata || {}
  const dataType = String(metadata.data_type || metadata.attributes?.item?.data_type || '').toLowerCase()
  const format = String(metadata.format || metadata.attributes?.item?.format || '').toLowerCase()
  const representation = String(metadata.representation || metadata.attributes?.item?.representation || '').toLowerCase()
  const layout = String(metadata.layout || metadata.attributes?.item?.layout || 'single').toLowerCase()
  const physicalPath = String(metadata.physical_path || metadata.attributes?.storage?.physical_path || '').trim()
  if (dataType !== 'table' || representation !== 'encoded' || !physicalPath || !OBJECT_TABLE_FORMATS.includes(format)) {
    return null
  }
  return {
    physical_path: physicalPath,
    layout,
    format
  }
}
