import { approvedBoundaryFields, readNativeApprovalFacts, serializeEngineCatalogTarget } from '@common-ui'

export function validateDirectPermission(row, engineID, fingerprint) {
  const validVersion = typeof row?.version === 'string' && /^(0|[1-9]\d{0,18})$/.test(row.version) && BigInt(row.version) <= 9223372036854775807n
  const validSource = row?.source === 'deployment_unknown' ? row.state === 'unknown' && row.version === '0'
    : ['explicit', 'deployment_default'].includes(row?.source) && ['allowed', 'denied'].includes(row.state) && row.version !== '0'
  if (!validVersion || !validSource || String(row.engine_id) !== String(engineID) || !/^[a-f0-9]{64}$/.test(row.item_fingerprint || '') ||
    (fingerprint && row.item_fingerprint !== fingerprint)) throw new Error('invalidDirectPermission')
  return row
}

// Reuse the canonical boundary validator, including exact file composition.
// The identity list is only navigation; it cannot supply a signing boundary.
export async function readPermissionTarget(engineID, fingerprint, readFacts) {
  const facts = await readFacts(engineID, fingerprint)
  const target = facts?.boundary?.native_target || facts?.boundary?.file_composition?.refs?.find(ref => ref.primary === true)?.target
  if (String(target?.engine_id) !== String(engineID) || facts?.item_fingerprint !== fingerprint) throw new Error('invalidDataItemBoundary')
  const observed = await readNativeApprovalFacts(target, async (id, fp) => {
    if (String(id) !== String(engineID) || fp !== fingerprint) throw new Error('invalidDataItemBoundary')
    return facts
  })
  if (observed.state !== 'complete') throw new Error('invalidDataItemBoundary')
  return { path: target, binding: observed.binding }
}

export function captureDirectPermission(row, target, allowed, reason) {
  validateDirectPermission(row, target.path.engine_id, target.binding.item_fingerprint)
  reason = typeof reason === 'string' ? reason.trim() : ''
  if (typeof allowed !== 'boolean' || !reason || Array.from(reason).length > 2000) throw new Error('invalidDirectPermission')
  const fields = approvedBoundaryFields(target.path, target.binding)
  return `{"catalog_path":${serializeEngineCatalogTarget({ ...target.path, engine_id: String(row.engine_id) })},"expected_boundary_hash":${JSON.stringify(fields.expected_boundary_hash)},"allowed":${allowed},"expected_source":${JSON.stringify(row.source)},"expected_version":${JSON.stringify(row.version)},"reason":${JSON.stringify(reason)}}`
}

// The tree is a complete navigation observation, never a permission credential.
export const MAX_PERMISSION_TREE_ITEMS = 20000
export async function readDirectPermissionTree(engineID, readPage, isCurrent = () => true) {
  const rows = [], size = 100
  let total, previous = ''
  for (let page = 1; ; page++) {
    if (!isCurrent()) throw new Error('stalePermissionTree')
    const result = await readPage(engineID, { page, page_size: size })
    if (!isCurrent()) throw new Error('stalePermissionTree')
    if (!Array.isArray(result?.data) || !Number.isSafeInteger(result.total) || result.total < 0 || result.page !== page || result.page_size !== size ||
      result.data.length !== Math.max(0, Math.min(size, result.total - (page - 1) * size)) || (total !== undefined && total !== result.total)) throw new Error('incompletePermissionTree')
    total = result.total
    if (total > MAX_PERMISSION_TREE_ITEMS) throw new Error('permissionTreeCapacity')
    for (const row of result.data) {
      validateDirectPermission(row, engineID)
      if (row.item_fingerprint <= previous) throw new Error('incompletePermissionTree')
      previous = row.item_fingerprint; rows.push(row)
    }
    if (rows.length === total) break
  }
  buildDirectPermissionTree(rows) // Validate the complete ancestor graph before exposing any checkbox.
  return rows
}

export function buildDirectPermissionTree(rows) {
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
    parent.children.push({ id: `item:${row.item_fingerprint}`, label: row.name, type: row.item_type, permission: row })
  }
  return roots
}

export function permissionNodeItems(node, recursive) {
  if (node.permission) return [node.permission]
  return (node.children || []).flatMap(child => child.permission ? [child.permission] : recursive ? permissionNodeItems(child, true) : [])
}

export function changedPermissionRows(rows, changes) {
  return rows.filter(row => changes.has(row.item_fingerprint) &&
    (row.state === 'unknown' || changes.get(row.item_fingerprint) !== (row.state === 'allowed')))
}
