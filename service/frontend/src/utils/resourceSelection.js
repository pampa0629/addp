import {
  isContentStorageEngine,
  isNativeTableStorageEngine,
  queryFederationCapability,
  supportsQueryLanguage
} from '@addp/common-frontend/basic/src/utils/engineCapabilities.mjs'

export const OBJECT_TABLE_FORMATS = ['parquet']

export function isNativeTableEngine(engine) {
  return isNativeTableStorageEngine(engine)
}

export function isPMTilesEngine(engine) {
  return isContentStorageEngine(engine) && engine?.is_builtin !== true
}

export function isQueryableTableEngine(engine, runtimes = []) {
  if (isNativeTableEngine(engine)) {
    return supportsQueryLanguage(engine, 'sql')
  }
  if (!isContentStorageEngine(engine)) return false
  const engineType = String(engine?.engine_type || '').trim().toLowerCase()
  return (runtimes || []).some(runtime => {
    const federation = queryFederationCapability(runtime)
    return federation?.sourceEngineTypes.has(engineType) &&
      OBJECT_TABLE_FORMATS.some(format => federation.objectFormats.has(format))
  })
}

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

export function isPMTilesNode(node) {
  if (!node) return false
  const type = String(node.type || '').toLowerCase()
  if (type !== 'object' && type !== 'file') return false
  const metadata = node.metadata || {}
  const dataType = String(metadata.data_type || metadata.attributes?.item?.data_type || '').toLowerCase()
  const format = String(metadata.format || metadata.attributes?.item?.format || '').toLowerCase()
  const layout = String(metadata.layout || metadata.attributes?.item?.layout || '').toLowerCase()
  return dataType === 'media' && format === 'pmtiles' && layout === 'single'
}

export function isPMTilesVisibleNode(node) {
  if (!node) return false
  if (isPMTilesNode(node)) return true
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
