// The tree is a complete navigation observation, never a permission credential.
export const MAX_IDENTITY_TREE_ITEMS = 20000
export async function readDataItemIdentityTree(engineID, readPage, isCurrent = () => true, validateRow = () => {}) {
  const rows = [], size = 100
  let total, previous = ''
  for (let page = 1; ; page++) {
    if (!isCurrent()) throw new Error('stalePermissionTree')
    const result = await readPage(engineID, { page, page_size: size })
    if (!isCurrent()) throw new Error('stalePermissionTree')
    if (!Array.isArray(result?.data) || !Number.isSafeInteger(result.total) || result.total < 0 || result.page !== page || result.page_size !== size ||
      result.data.length !== Math.max(0, Math.min(size, result.total - (page - 1) * size)) || (total !== undefined && total !== result.total)) throw new Error('incompletePermissionTree')
    total = result.total
    if (total > MAX_IDENTITY_TREE_ITEMS) throw new Error('permissionTreeCapacity')
    for (const row of result.data) {
      validateRow(row, engineID)
      if (String(row.engine_id) !== String(engineID) || !/^[a-f0-9]{64}$/.test(row.item_fingerprint || '')) throw new Error('invalidPermissionTree')
      if (row.item_fingerprint <= previous) throw new Error('incompletePermissionTree')
      previous = row.item_fingerprint; rows.push(row)
    }
    if (rows.length === total) break
  }
  buildDataItemIdentityTree(rows) // Validate the complete ancestor graph before exposing any checkbox.
  return rows
}

export function buildDataItemIdentityTree(rows) {
  const nodes = new Map(), roots = [], fingerprints = new Set()
  for (const row of rows) {
    if (fingerprints.has(row.item_fingerprint) || !Array.isArray(row.ancestors) || !row.ancestors.length || row.ancestors.length > 128 || !row.name || !row.full_name) throw new Error('invalidPermissionTree')
    fingerprints.add(row.item_fingerprint)
    let parent = null
    const seen = new Set()
    for (const ancestor of row.ancestors) {
      if (!/^[1-9]\d*$/.test(ancestor.id) || typeof ancestor.name !== 'string' || !ancestor.node_type || seen.has(ancestor.id)) throw new Error('invalidPermissionTree')
      seen.add(ancestor.id)
      const id = `node:${ancestor.id}`, parentID = parent?.id || ''
      let node = nodes.get(id)
      if (node) {
        if (node.parentID !== parentID || node.label !== ancestor.name || node.type !== ancestor.node_type) throw new Error('invalidPermissionTree')
      } else {
        node = { id, label: ancestor.name, type: ancestor.node_type, parentID, children: [] }
        nodes.set(id, node); (parent ? parent.children : roots).push(node)
      }
      parent = node
    }
    parent.children.push({ id: `item:${row.item_fingerprint}`, label: row.name, type: row.item_type, item: row })
  }
  return roots
}
