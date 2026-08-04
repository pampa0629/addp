export function isNativeTargetSelectable(node, context = {}) {
  const type = String(node?.type || '').trim().toLowerCase()
  if (['schema', 'database'].includes(type)) {
    return !!context.locator?.nodeId
  }
  return type === 'table' && !!context.locator?.itemId
}

export function sameNativeTargetParentIdentity(left, right) {
  if (!left?.engineID || !right?.engineID) return false
  if (left.engineID !== right.engineID || left.type !== right.type) return false
  if (!Array.isArray(left.path) || !Array.isArray(right.path) || left.path.length !== right.path.length) {
    return false
  }
  return left.path.every((part, index) => part === right.path[index])
}
