export const ROOT_CATEGORY_PARENT = '__root__'

export function collectCategorySubtreeIds(node, result = new Set()) {
  if (!node) return result
  result.add(node.id)
  for (const child of node.children || []) collectCategorySubtreeIds(child, result)
  return result
}

export function buildCategoryParentOptions(nodes, excludedIds = new Set(), path = []) {
  const options = []
  for (const node of nodes || []) {
    const currentPath = [...path, node.name]
    if (!excludedIds.has(node.id)) {
      options.push({ value: node.id, label: currentPath.join(' / ') })
    }
    options.push(...buildCategoryParentOptions(node.children, excludedIds, currentPath))
  }
  return options
}

export function findCategoryNode(nodes, id) {
  for (const node of nodes || []) {
    if (node.id === id) return node
    const child = findCategoryNode(node.children, id)
    if (child) return child
  }
  return null
}
