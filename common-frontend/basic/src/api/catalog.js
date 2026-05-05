/**
 * System 实时 catalog 浏览 API。
 *
 * 实时 catalog 统一由 System 提供；Meta、Manager 等模块只复用这里的
 * client 和节点适配，不再直接调用 Meta 的历史实时浏览接口。
 */

export function normalizeCatalogPath(path = { segments: [] }) {
  if (typeof path === 'string') {
    return browserPathToCatalogPath(path)
  }
  return {
    segments: [],
    ...path,
    segments: Array.isArray(path?.segments) ? path.segments : []
  }
}

export async function listCatalogChildren(client, engineId, path = { segments: [] }, options = {}) {
  const catalogPath = normalizeCatalogPath(path)
  const res = await client.post(`/system/engines/${engineId}/catalog/children`, {
    path: catalogPath,
    options
  })
  return Array.isArray(res?.nodes) ? res.nodes : []
}

export async function listCatalogBrowserNodes(client, engineId, path = { segments: [] }, options = {}) {
  const nodes = await listCatalogChildren(client, engineId, path, options)
  return nodes.map(toCatalogBrowserNode)
}

export function browserPathToCatalogPath(path = '') {
  const segments = String(path)
    .split('/')
    .map(part => part.trim())
    .filter(Boolean)
    .map((name, index) => ({
      name,
      term: index === 0 ? 'bucket' : 'prefix',
      kind: index === 0 ? 'bucket' : 'prefix'
    }))
  return { segments }
}

export function catalogPathToString(path) {
  const segments = Array.isArray(path?.segments) ? path.segments : []
  return segments.map(segment => segment.name).filter(Boolean).join('/')
}

function catalogAttributeValue(attributes = {}, section, key) {
  return attributes?.[section]?.[key] || attributes?.[key]
}

export function toCatalogBrowserNode(node) {
  const nodePath = catalogAttributeValue(node.attributes, 'storage', 'path') || catalogPathToString(node.path) || node.name
  const type = catalogNodeBrowserType(node)
  return {
    name: node.name,
    schema_name: node.name,
    path: nodePath,
    catalog_path: node.path,
    term: node.term,
    kind: node.kind,
    type,
    node_type: type,
    is_container: !!node.is_container,
    is_item: !!node.is_item,
    size_bytes: node.stats?.size_bytes,
    stats: node.stats || {},
    attributes: node.attributes || {},
    file_type: type === 'file' || type === 'object' ? fileExtension(node.name) : ''
  }
}

export function catalogNodeBrowserType(node) {
  if (['bucket', 'root', 'prefix', 'object', 'file'].includes(node.kind)) {
    return node.kind
  }
  if (node.kind === 'namespace') {
    return node.term || 'namespace'
  }
  return node.is_container ? (node.term || 'prefix') : (node.term || 'object')
}

function fileExtension(name = '') {
  const index = name.lastIndexOf('.')
  return index >= 0 ? name.slice(index) : ''
}
