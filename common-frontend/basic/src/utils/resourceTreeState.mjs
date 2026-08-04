function uniqueKeys(keys = []) {
  return [...new Set((Array.isArray(keys) ? keys : []).filter(key => key !== null && key !== undefined && key !== ''))]
}

export function defaultExpandedKeys(treeData = [], options = {}) {
  const { expandAll = false, expandRoot = false } = options
  if (expandAll) {
    const keys = []
    const visit = (nodes) => {
      for (const node of nodes || []) {
        if (node?.id !== null && node?.id !== undefined && node?.id !== '') {
          keys.push(node.id)
        }
        visit(node?.children)
      }
    }
    visit(treeData)
    return uniqueKeys(keys)
  }
  if (expandRoot) {
    return uniqueKeys((treeData || []).map(node => node?.id))
  }
  return []
}

export function resolveExpandedKeys({
  override = null,
  expandedKeys = [],
  treeData = [],
  expandAll = false,
  expandRoot = false
} = {}) {
  if (override !== null) {
    return uniqueKeys(override)
  }
  if (Array.isArray(expandedKeys) && expandedKeys.length > 0) {
    return uniqueKeys(expandedKeys)
  }
  return defaultExpandedKeys(treeData, { expandAll, expandRoot })
}

export function addExpandedKey(keys, key) {
  return uniqueKeys([...(Array.isArray(keys) ? keys : []), key])
}

export function removeExpandedKey(keys, key) {
  return uniqueKeys(keys).filter(candidate => candidate !== key)
}

export function hasExpandableChildren(node) {
  if (!node) return false
  if (Array.isArray(node.children) && node.children.length > 0) return true
  if (node.hasChildren === true) return true
  if (node.metadata?.has_children === true) return true
  return Number(node.metadata?.item_count || 0) > 0
}
