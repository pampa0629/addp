import { parseLocator } from '@addp/common-frontend'

const catalogRootTypes = new Set(['root', 'server', 'service'])

const locatorFromNode = (node) => node?.locator || node?.id || ''

const isSyntheticCatalogRoot = (node, locator) => {
  if (!node || !catalogRootTypes.has(node.type)) return false
  const loc = parseLocator(locator)
  return loc.path.length === 0 && !loc.nodeId && !loc.itemId
}

/**
 * 将前端根据 Engine 构造的临时 catalog root 转换成 Meta 返回的规范节点。
 * 规范 root 必须携带 node_id；临时 locator 不能进入路由或节点级 API。
 */
export const resolveCanonicalNodeSelection = async ({ node, locator, loadTree }) => {
  if (!isSyntheticCatalogRoot(node, locator)) {
    return { node, locator }
  }

  const loc = parseLocator(locator)
  const canonicalNode = await loadTree(loc.engineId)
  const canonicalLocator = locatorFromNode(canonicalNode)
  const canonicalLoc = canonicalLocator ? parseLocator(canonicalLocator) : null

  if (!canonicalNode || canonicalLoc?.engineId !== loc.engineId || !canonicalLoc?.nodeId) {
    throw new Error('resource tree root is missing node_id')
  }

  return {
    node: canonicalNode,
    locator: canonicalLocator,
  }
}
