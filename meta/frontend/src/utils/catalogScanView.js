export function isDirectLeafCatalog(resource) {
  const topTerm = String(resource?.catalog_top_term || '').toLowerCase()
  const leafTerm = String(resource?.catalog_leaf_term || '').toLowerCase()
  return topTerm !== '' && topTerm === leafTerm
}

export function selectLiveCatalogTopEntries(entries, resource) {
  const expectedRole = isDirectLeafCatalog(resource) ? 'leaf' : 'branch'
  return (Array.isArray(entries) ? entries : []).filter(entry => entry?.role === expectedRole)
}

export function selectScannedCatalogTopEntries(tree, resource) {
  const root = findCatalogRootNode(tree)
  if (!root) return []

  if (isDirectLeafCatalog(resource)) {
    const items = Array.isArray(tree?.items) ? tree.items : []
    return items
      .filter(item => item.node_id === root.id)
      .map(item => ({
        id: item.id,
        name: item.name,
        item_type: item.item_type,
        node_type: item.item_type,
        path: item.full_name || item.name,
        role: 'leaf',
        scan_status: item.scanned_at ? 'completed' : 'pending',
        scanned_depth: item.scanned_depth || '',
        scanned_at: item.scanned_at || '',
        item_count: 1,
        total_size_bytes: item.size_bytes || 0
      }))
  }

  const childNodes = Array.isArray(tree?.child_nodes) ? tree.child_nodes : []
  return childNodes
    .filter(node => node.parent_node_id === root.id)
    .map(node => ({
      id: node.id,
      name: node.name,
      node_type: node.node_type,
      path: node.full_name || node.name,
      role: 'branch',
      scan_status: node.scan_status,
      scanned_depth: node.scanned_depth,
      scanned_at: node.scanned_at,
      item_count: node.item_count || 0,
      total_size_bytes: node.total_size_bytes || 0
    }))
}

function findCatalogRootNode(tree) {
  const roots = Array.isArray(tree?.top_nodes) ? tree.top_nodes : []
  return roots.find(node => String(node?.full_name || '').trim() === '')
}
