const resourceRootTypes = new Set(['root', 'server', 'service'])

export function createResourceRootNode(engine) {
  const locator = `addp://engine/${engine.id}/path/?type=root`
  return {
    id: locator,
    locator,
    label: engine.name,
    type: 'root',
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
    schema,
    table
  }
}

export function geometryColumnsFromNode(node) {
  const spatial = node?.metadata?.spatial || {}
  const columns = []
  if (spatial.geometry) columns.push(spatial.geometry)
  if (Array.isArray(spatial.geometry_columns)) {
    columns.push(...spatial.geometry_columns)
  }
  return columns.map((column) => String(column || '').trim()).filter(Boolean)
}

export function findResourceNodePath(nodes, locator, path = []) {
  for (const node of nodes || []) {
    const nodeLocator = node.locator || node.id
    const nextPath = [...path, node]
    if (nodeLocator === locator) {
      return nextPath
    }
    if (node.children?.length) {
      const found = findResourceNodePath(node.children, locator, nextPath)
      if (found.length) {
        return found
      }
    }
  }
  return []
}

export function locatorEngineID(locator, parseLocator) {
  return Number(parseLocator?.(locator)?.engineId || 0)
}
