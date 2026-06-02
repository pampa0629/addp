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
  return catalogNodesFromResponse(res)
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

export function catalogNodesFromResponse(response) {
  if (Array.isArray(response?.nodes)) return response.nodes
  if (Array.isArray(response?.data?.nodes)) return response.data.nodes
  if (Array.isArray(response)) return response
  return []
}

export function toCatalogBrowserNode(node) {
  const nodePath = catalogPathToString(node.path) || node.name
  const type = catalogNodeBrowserType(node)
  const isContainer = node.role === 'branch'
  const isItem = node.role === 'leaf'
  const sizeBytes = node.storage?.size_bytes ?? node.table?.size_bytes
  return {
    name: node.name,
    schema_name: node.name,
    path: nodePath,
    catalog_path: node.path,
    term: node.term,
    kind: node.kind,
    role: node.role,
    type,
    node_type: type,
    is_container: isContainer,
    is_item: isItem,
    size_bytes: sizeBytes,
    table: node.table,
    storage: node.storage,
    leaf_count: node.leaf_count,
    updated_at: node.updated_at,
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
  if (node.role === 'branch') {
    return node.term || 'prefix'
  }
  return node.term || 'object'
}

function fileExtension(name = '') {
  const index = name.lastIndexOf('.')
  return index >= 0 ? name.slice(index) : ''
}
