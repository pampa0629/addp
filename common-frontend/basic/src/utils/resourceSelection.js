import { formatLocatorDisplayPath, parseLocator } from '../types/resourceLocator.js'

function geometryColumnName(column) {
  return String(typeof column === 'string' ? column : column?.name || '').trim()
}

export function spatialFactsFromResourceTreeMetadata(metadata = {}) {
  const spatial = metadata?.spatial || {}
  const columns = []
  const seen = new Set()
  for (const column of spatial.geometry_columns || []) {
    const name = geometryColumnName(column)
    if (!name || seen.has(name)) continue
    seen.add(name)
    columns.push(name)
  }
  const declaredPrimary = geometryColumnName(spatial.primary_geometry_column)
  const primary = declaredPrimary && seen.has(declaredPrimary) ? declaredPrimary : (columns[0] || '')
  return {
    geometry_columns: columns,
    primary_geometry_column: primary
  }
}

export function geometryColumnFactsFromSelection(selection) {
  const spatial = selection?.resource?.spatial || {}
  const columns = Array.from(new Set((spatial.geometry_columns || []).map(geometryColumnName).filter(Boolean)))
  const declaredPrimary = geometryColumnName(spatial.primary_geometry_column)
  return {
    columns,
    selected: declaredPrimary && columns.includes(declaredPrimary) ? declaredPrimary : (columns[0] || '')
  }
}

export function selectionFromResourceTreeNode(node, engine = null) {
  if (!node?.locator) return null
  const parsed = parseLocator(node.locator)
  const metadata = node.metadata || {}
  const engineType = engine?.engine_type || metadata.engine_type || ''
  return {
    identity: {
      locator: node.locator,
      engine_id: parsed.engineId,
      node_id: parsed.nodeId || metadata.node_id,
      item_id: parsed.itemId || metadata.item_id
    },
    display: {
      label: node.label,
      path: formatLocatorDisplayPath(node.locator, { engineType }),
      type: node.type,
      engine_name: engine?.name || metadata.engine_name,
      engine_type: engineType
    },
    resource: {
      kind: parsed.itemId ? 'item' : 'node',
      type: node.type,
      data_type: metadata.data_type,
      format: metadata.format,
      representation: metadata.representation,
      spatial: spatialFactsFromResourceTreeMetadata(metadata)
    },
    raw: { engine, node }
  }
}
