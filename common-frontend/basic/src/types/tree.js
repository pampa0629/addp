/**
 * 树形组件相关类型定义和工具函数
 */

/**
 * 默认节点图标映射
 */
export const DEFAULT_NODE_ICONS = {
  resource: 'Collection',
  schema: 'OfficeBuilding',
  database: 'OfficeBuilding',
  bucket: 'Folder',
  directory: 'Folder',
  table: 'Document',
  object: 'Document',
  task: 'Operation',
  folder: 'FolderOpened',
  file: 'Document'
}

/**
 * @deprecated 使用 ResourceLocator URI 代替 makeNodeId
 * 原 makeNodeId 和 sanitizeNodeId 函数已删除
 * 请使用 parseLocator() 和 buildLocator() from '@addp/common-frontend'
 */

/**
 * 扁平化树结构
 * @param {Array} nodes - 树节点数组
 * @returns {Array} 扁平化后的节点数组
 */
export function flattenTree(nodes) {
  const result = []
  const traverse = (nodes) => {
    for (const node of nodes) {
      result.push(node)
      if (node.children?.length) {
        traverse(node.children)
      }
    }
  }
  traverse(nodes || [])
  return result
}

/**
 * 查找节点路径（从根到目标节点的所有 ID）
 * @param {Array} nodes - 树节点数组
 * @param {string|number} targetId - 目标节点 ID
 * @returns {Array} 路径数组
 */
export function findNodePath(nodes, targetId) {
  const path = []
  const traverse = (nodes, currentPath) => {
    for (const node of nodes) {
      const newPath = [...currentPath, node.id]
      if (node.id === targetId) {
        path.push(...newPath)
        return true
      }
      if (node.children?.length && traverse(node.children, newPath)) {
        return true
      }
    }
    return false
  }
  traverse(nodes || [], [])
  return path
}

/**
 * 通过 ID 查找节点
 * @param {Array} nodes - 树节点数组
 * @param {string|number} targetId - 目标节点 ID
 * @returns {Object|null} 找到的节点或 null
 */
export function findNodeById(nodes, targetId) {
  const traverse = (nodes) => {
    for (const node of nodes) {
      if (node.id === targetId) {
        return node
      }
      if (node.children?.length) {
        const found = traverse(node.children)
        if (found) return found
      }
    }
    return null
  }
  return traverse(nodes || [])
}

/**
 * 获取所有父节点的 ID
 * @param {Array} nodes - 树节点数组
 * @param {string|number} targetId - 目标节点 ID
 * @returns {Array} 父节点 ID 数组（不包含目标节点自己）
 */
export function getParentIds(nodes, targetId) {
  const path = findNodePath(nodes, targetId)
  return path.slice(0, -1)
}

/**
 * 获取所有叶子节点
 * @param {Array} nodes - 树节点数组
 * @returns {Array} 叶子节点数组
 */
export function getLeafNodes(nodes) {
  const leaves = []
  const traverse = (nodes) => {
    for (const node of nodes) {
      if (!node.children || node.children.length === 0) {
        leaves.push(node)
      } else {
        traverse(node.children)
      }
    }
  }
  traverse(nodes || [])
  return leaves
}

/**
 * 过滤树节点
 * @param {Array} nodes - 树节点数组
 * @param {Function} predicate - 过滤函数 (node) => boolean
 * @returns {Array} 过滤后的树
 */
export function filterTree(nodes, predicate) {
  const filtered = []
  for (const node of nodes || []) {
    const match = predicate(node)
    let filteredChildren = []

    if (node.children?.length) {
      filteredChildren = filterTree(node.children, predicate)
    }

    if (match || filteredChildren.length) {
      filtered.push({
        ...node,
        children: filteredChildren
      })
    }
  }
  return filtered
}

/**
 * 遍历树节点
 * @param {Array} nodes - 树节点数组
 * @param {Function} callback - 回调函数 (node, parent, level) => void
 */
export function traverseTree(nodes, callback) {
  const traverse = (nodes, parent = null, level = 0) => {
    for (const node of nodes || []) {
      callback(node, parent, level)
      if (node.children?.length) {
        traverse(node.children, node, level + 1)
      }
    }
  }
  traverse(nodes)
}

/**
 * 计算树的深度
 * @param {Array} nodes - 树节点数组
 * @returns {number} 树的最大深度
 */
export function getTreeDepth(nodes) {
  let maxDepth = 0
  const traverse = (nodes, depth = 0) => {
    if (!nodes || nodes.length === 0) return
    maxDepth = Math.max(maxDepth, depth)
    for (const node of nodes) {
      if (node.children?.length) {
        traverse(node.children, depth + 1)
      }
    }
  }
  traverse(nodes, 1)
  return maxDepth
}

/**
 * 克隆树结构
 * @param {Array} nodes - 树节点数组
 * @returns {Array} 克隆后的树
 */
export function cloneTree(nodes) {
  return (nodes || []).map((node) => ({
    ...node,
    children: node.children ? cloneTree(node.children) : []
  }))
}

/**
 * 获取树中所有节点的 ID
 * @param {Array} nodes - 树节点数组
 * @returns {Array} 所有节点 ID 数组
 */
export function getAllNodeKeys(nodes) {
  const keys = []
  const traverse = (nodes) => {
    for (const node of nodes) {
      keys.push(node.id)
      if (node.children?.length) {
        traverse(node.children)
      }
    }
  }
  traverse(nodes || [])
  return keys
}

/**
 * 树节点排序
 * @param {Array} nodes - 树节点数组
 * @param {Function} compareFn - 比较函数
 * @param {boolean} recursive - 是否递归排序子节点
 * @returns {Array} 排序后的树
 */
export function sortTree(nodes, compareFn, recursive = true) {
  const sorted = [...(nodes || [])].sort(compareFn)
  if (recursive) {
    return sorted.map((node) => ({
      ...node,
      children: node.children ? sortTree(node.children, compareFn, true) : []
    }))
  }
  return sorted
}
