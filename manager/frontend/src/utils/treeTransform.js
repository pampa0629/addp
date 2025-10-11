/**
 * 树形数据转换工具
 */

const OBJECT_STORAGE_TYPES = ['s3', 'minio', 'oss', 'object_storage', 'object-storage']

export const isObjectStorageResource = (type) => {
  return OBJECT_STORAGE_TYPES.includes(String(type || '').toLowerCase())
}

const sanitizeNodeId = (value) =>
  String(value ?? '')
    .trim()
    .replace(/[^a-zA-Z0-9_-]+/g, '-')

export const makeNodeId = (...parts) => {
  const cleaned = parts
    .filter((part) => part !== undefined && part !== null && String(part).length)
    .map((part) => sanitizeNodeId(part))
    .filter((part) => part.length)
  if (cleaned.length === 0) {
    return `node-${Math.random().toString(36).slice(2)}`
  }
  return cleaned.join('-')
}

export const transformTableNode = (resource, schemaName, table) => {
  const nodeType = (table.type || table.node_type || table.nodeType || 'table').toLowerCase()
  const fullName = table.full_name || table.fullName || ''
  let path = fullName || table.path || ''

  // 对于对象存储，如果path包含bucket名称前缀，去掉它
  const isObjectStorage = isObjectStorageResource(resource.resource_type || resource.resourceType)
  if (isObjectStorage && path && path.startsWith(schemaName + '/')) {
    path = path.substring(schemaName.length + 1)
  }

  const sizeBytes = table.size_bytes ?? table.sizeBytes ?? 0
  const objectCount = table.object_count ?? table.objectCount ?? 0
  const contentType = table.content_type ?? table.contentType ?? ''

  const node = {
    id: makeNodeId(nodeType, resource.id, schemaName, fullName || table.name || table.id || Math.random()),
    type: nodeType,
    nodeType,
    label: table.name,
    resourceId: resource.id,
    resourceType: resource.resource_type || resource.resourceType,
    schema: schemaName,
    table: nodeType === 'table' ? table.name : path,
    path,
    fullName,
    parentPath: table.parent_path || table.parentPath || '',
    sizeBytes,
    objectCount,
    contentType,
    children: []
  }

  if (Array.isArray(table.children) && table.children.length > 0) {
    node.children = table.children.map((child) => transformTableNode(resource, schemaName, child))
  }

  if (nodeType === 'directory' || nodeType === 'bucket') {
    node.table = path
    node.path = path
  }
  if (nodeType === 'object') {
    node.table = path
    node.path = path
  }

  if (nodeType === 'table') {
    node.type = 'table'
  }

  return node
}

export const transformResource = (resource) => {
  const resourceType = resource.resource_type || resource.resourceType
  const isObjectStorage = isObjectStorageResource(resourceType)
  const schemas = (resource.schemas || []).map((schema) => {
    const nodeType = isObjectStorage ? 'bucket' : 'schema'
    const schemaNode = {
      id: makeNodeId(nodeType, resource.id, schema.name),
      type: nodeType,
      nodeType,
      label: schema.name,
      resourceId: resource.id,
      resourceType,
      schema: schema.name,
      table: '',
      path: '',
      children: []
    }
    const tables = schema.tables || []
    schemaNode.children = tables.map((table) => transformTableNode(resource, schema.name, table))
    return schemaNode
  })

  return {
    id: makeNodeId('resource', resource.id),
    type: 'resource',
    nodeType: 'resource',
    label: resource.name,
    resourceId: resource.id,
    resourceType,
    children: schemas
  }
}
