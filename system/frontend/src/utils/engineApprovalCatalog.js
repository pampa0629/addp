import { createEngineCatalogTreeAdapter, safePickerEngineID } from '@common-ui'

const isFileModelEngine = kind => kind === 'ftp' || kind === 'nfs' || kind === 'minio'

export function isApprovalTable(node) {
  const entry = node?.metadata?.catalog_entry
  const leaf = entry?.path?.segments?.at(-1)
  return entry?.role === 'leaf' && entry.term === 'table' && entry.kind === 'table' &&
    entry.path?.version === 'catalog.path/v1' && leaf?.term === 'table' && leaf.kind === 'table'
}

export function isApprovalSelection(node) {
  const entry = node?.metadata?.catalog_entry
  return isApprovalTarget(node) || (!isFileModelEngine(node?.metadata?.engine_type) && entry?.role === 'branch' && entry.path?.segments?.length > 1)
}

export const isApprovalTarget = node => isApprovalTable(node) || isApprovalModel(node)

export function isApprovalModel(node) {
  const entry = node?.metadata?.catalog_entry, leaf = entry?.path?.segments?.at(-1)
  const engineType = node?.metadata?.engine_type
  const term = engineType === 'minio' ? 'object' : 'file'
  const suffix = /\.(ifc|3ds|dae|obj|stl|glb|gltf)$/i
  return isFileModelEngine(engineType) && entry?.role === 'leaf' && entry.term === term && entry.kind === term &&
    entry.path?.version === 'catalog.path/v1' && leaf?.term === term && leaf.kind === term && suffix.test(leaf.name)
}

// A namespace is a discovery shortcut, never an authorization target.
// Return only a complete bounded snapshot; failures discard the whole selection.
export async function collectApprovalTargets(selection, engineID, adapter, limit = 200) {
  const targets = [], visited = new Set(), root = selection?.raw?.node
  if (!isApprovalSelection(root)) throw new Error('invalidCatalogTarget')
  async function visit(node, parent) {
    const entry = node?.metadata?.catalog_entry, path = entry?.path
    const key = JSON.stringify(path)
    if (!path || String(path.engine_id) !== String(engineID) || visited.has(key) || path.segments.length > 64 ||
      (parent && (path.segments.length !== parent.segments.length + 1 || !parent.segments.every((part, index) =>
        ['term', 'kind', 'name'].every(field => part[field] === path.segments[index]?.[field]))))) throw new Error('invalidCatalogTarget')
    visited.add(key)
    if (isApprovalTarget(node)) {
      targets.push(approvalTargetFromSelection({ raw: { node } }, engineID))
      if (targets.length > limit) throw new Error('tooManyTargets')
    } else if (entry.role === 'branch') {
      const children = await adapter.getNodeChildren(node)
      for (const child of children) await visit(child, path)
    }
  }
  await visit(root)
  return targets
}

export function approvalTargetFromSelection(selection, engineID) {
  const entry = selection?.raw?.node?.metadata?.catalog_entry
  const id = safePickerEngineID(engineID)
  if (!isApprovalTarget(selection?.raw?.node) || entry.path?.engine_id !== id || !Array.isArray(entry.path?.segments) || entry.path.segments.length < 2) {
    throw new Error('invalidCatalogTarget')
  }
  return { version: entry.path.version, engine_id: String(id), segments: entry.path.segments.map(segment => ({ ...segment })) }
}

export function approvalPathLabel(path) {
  return (path?.segments || []).filter(segment => segment.name !== '').map(segment => segment.name).join(' / ')
}

export function approvalTargetsEqual(left, right) {
  return !!left && !!right && String(left.engine_id) === String(right.engine_id) && left.version === right.version &&
    Array.isArray(left.segments) && Array.isArray(right.segments) && left.segments.length === right.segments.length &&
    left.segments.every((segment, index) => ['term', 'kind', 'name'].every(key =>
      typeof segment?.[key] === 'string' && segment[key] === right.segments[index]?.[key]))
}

// System keeps approval policy; native browsing and tree conversion have one owner.
export function createApprovalCatalogAdapter(engine, listChildren) {
  return createEngineCatalogTreeAdapter(engine, async (id, path) => {
    const result = await listChildren(id, path)
    if (!Array.isArray(result?.nodes)) throw new Error('invalidCatalogTarget')
    return result.nodes
  }, { isSearchable: isApprovalSelection })
}
