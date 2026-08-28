import { catalogRootTypeForEngine, engineRootLocator } from '@addp/common-frontend'

const resourceRootTypes = new Set(['root', 'server', 'service'])

export function createResourceRootNode(engine) {
  const type = catalogRootTypeForEngine(engine)
  const locator = engineRootLocator(engine, type)
  return {
    id: locator,
    locator,
    label: engine.name,
    type,
    icon: 'Folder',
    engineId: engine.id,
    engineType: engine.engine_type,
    engineName: engine.name,
    children: [],
    hasChildren: true,
    loaded: false
  }
}

export function normalizeResourceNode(node, engine, { parseLocator, loaded = true } = {}) {
  if (!node) return null
  const locator = node.locator || node.id || ''
  const metadata = node.metadata || {}
  return {
    ...node,
    id: locator || node.id,
    locator,
    label: node.label || node.name || engine?.name || locator,
    engineId: node.engineId || node.engine_id || metadata.engine_id || engine?.id || locatorEngineID(locator, parseLocator),
    engineType: node.engineType || node.engine_type || metadata.engine_type || engine?.engine_type || '',
    engineName: node.engineName || node.engine_name || engine?.name || '',
    loaded,
    children: Array.isArray(node.children)
      ? node.children.map((child) => normalizeResourceNode(child, engine, { parseLocator })).filter(Boolean)
      : []
  }
}

export function replaceResourceNode(nodes, locator, replacement) {
  return nodes.map((node) => {
    const nodeLocator = node.locator || node.id
    if (nodeLocator === locator) {
      return replacement
    }
    if (node.children?.length) {
      return {
        ...node,
        children: replaceResourceNode(node.children, locator, replacement)
      }
    }
    return node
  })
}

export function updateResourceNodeChildren(nodes, locator, children) {
  return nodes.map((node) => {
    const nodeLocator = node.locator || node.id
    if (nodeLocator === locator) {
      return {
        ...node,
        children,
        hasChildren: children.length > 0 || node.hasChildren
      }
    }
    if (node.children?.length) {
      return {
        ...node,
        children: updateResourceNodeChildren(node.children, locator, children)
      }
    }
    return node
  })
}

export function mergeAncestorChainIntoResourceTree(nodes, chain, { engine = null, parseLocator = null, filterNode = null } = {}) {
  const normalizedChain = (Array.isArray(chain) ? chain : [])
    // ancestors 只证明路径存在，不代表任一容器的直接子节点已经完整加载。
    .map((node) => normalizeResourceNode(node, engine, { parseLocator, loaded: false }))
    .filter(Boolean)
    .filter((node) => typeof filterNode !== 'function' || filterNode(node))

  if (!normalizedChain.length) {
    return { nodes, path: [], expandedKeys: [] }
  }

  const root = normalizedChain[0]
  let nextNodes = Array.isArray(nodes) ? [...nodes] : []
  const rootIndex = nextNodes.findIndex((node) => sameResourceNode(node, root))
  if (rootIndex < 0) {
    nextNodes.push(root)
  } else {
    nextNodes[rootIndex] = mergeResourceNodeFacts(nextNodes[rootIndex], root)
  }

  let current = rootIndex < 0 ? nextNodes[nextNodes.length - 1] : nextNodes[rootIndex]
  const path = [current]

  for (const node of normalizedChain.slice(1)) {
    current.children = upsertResourceChild(current.children || [], node)
    current.hasChildren = true
    current = findDirectResourceChild(current.children, node)
    path.push(current)
  }

  return {
    nodes: nextNodes,
    path,
    expandedKeys: path.slice(0, -1).map((node) => node.locator || node.id).filter(Boolean),
    target: path[path.length - 1] || null
  }
}

export function isResourceRootNode(node) {
  const locator = String(node?.locator || node?.id || '')
  return resourceRootTypes.has(String(node?.type || '').toLowerCase()) && locator.includes('/path/?')
}

export function tableSelectionFromResourceNode(node, parseLocator) {
  if (!node) return null
  const locator = node.locator || node.id || ''
  const loc = parseLocator?.(locator)
  const type = String(node.type || loc?.type || '').toLowerCase()
  if (type !== 'table') return null
  const path = loc?.path || []
  const schema = String(node.schema || path[path.length - 2] || '').trim()
  const table = String(node.table || path[path.length - 1] || '').trim()
  const engineID = Number(node.engineId || loc?.engineId || 0)
  if (!engineID || !schema || !table) return null
  return {
    source_engine_id: engineID,
    item_id: Number(loc?.itemId || node.metadata?.item_id || 0) || undefined,
    item_fingerprint: String(node.metadata?.item_fingerprint || '').trim(),
    locator,
    source_kind: type,
    full_name: path.join('/'),
    schema,
    table
  }
}

export function geometryColumnsFromNode(node) {
  const spatial = node?.metadata?.spatial || {}
  const columns = []
  if (Array.isArray(spatial.geometry_columns)) {
    columns.push(...spatial.geometry_columns)
  }
  if (spatial.primary_geometry_column) columns.unshift(spatial.primary_geometry_column)
  return [...new Set(columns.map((column) => String(column || '').trim()).filter(Boolean))]
}

export function locatorEngineID(locator, parseLocator) {
  return Number(parseLocator?.(locator)?.engineId || 0)
}

function sameResourceNode(a, b) {
  const aLocator = a?.locator || a?.id || ''
  const bLocator = b?.locator || b?.id || ''
  if (aLocator === bLocator) {
    return true
  }
  const aRootKey = catalogRootKey(a)
  const bRootKey = catalogRootKey(b)
  return Boolean(aRootKey && bRootKey && aRootKey === bRootKey)
}

function mergeResourceNodeFacts(existing, incoming) {
  const existingChildren = Array.isArray(existing?.children) ? existing.children : []
  const incomingChildren = Array.isArray(incoming?.children) ? incoming.children : []
  return {
    ...existing,
    ...incoming,
    loaded: existing?.loaded === true || incoming?.loaded === true,
    children: existingChildren.length ? existingChildren : incomingChildren
  }
}

function upsertResourceChild(children, child) {
  const index = children.findIndex((node) => sameResourceNode(node, child))
  if (index < 0) {
    return [...children, child]
  }
  const next = [...children]
  next[index] = mergeResourceNodeFacts(next[index], child)
  return next
}

function findDirectResourceChild(children, target) {
  return (children || []).find((node) => sameResourceNode(node, target)) || target
}

function catalogRootKey(node) {
  const locator = String(node?.locator || node?.id || '')
  const type = String(node?.type || '').toLowerCase()
  if (!resourceRootTypes.has(type) || !locator.includes('/path/?')) {
    return ''
  }
  const match = locator.match(/^addp:\/\/engine\/(\d+)\/path\/\?/)
  return match ? `engine:${match[1]}:root` : ''
}
