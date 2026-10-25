import { buildLocator } from '@common-ui/types/resourceLocator'

const isFileModelEngine = kind => kind === 'ftp' || kind === 'nfs' || kind === 'minio'

// Shared ResourceTreePicker currently models Engine IDs as safe JS numbers.
// Reject an unrepresentable ID before browsing; never select a rounded target.
export function safePickerEngineID(id) {
  const value = String(id)
  const numeric = Number(value)
  if (!/^[1-9]\d*$/.test(value) || !Number.isSafeInteger(numeric) || String(numeric) !== value) throw new Error('invalidCatalogTarget')
  return numeric
}

export function isApprovalTable(node) {
  const entry = node?.metadata?.catalog_entry
  const leaf = entry?.path?.segments?.at(-1)
  return entry?.role === 'leaf' && entry.term === 'table' && entry.kind === 'table' &&
    entry.path?.version === 'catalog.path/v1' && leaf?.term === 'table' && leaf.kind === 'table'
}

export function isApprovalSelection(node) {
  const entry = node?.metadata?.catalog_entry
  return isApprovalTarget(node) || (!isFileModelEngine(node?.metadata?.approval_engine_type) && entry?.role === 'branch' && entry.path?.segments?.length > 1)
}

export const isApprovalTarget = node => isApprovalTable(node) || isApprovalModel(node)

export function isApprovalModel(node) {
  const entry = node?.metadata?.catalog_entry, leaf = entry?.path?.segments?.at(-1)
  const engineType = node?.metadata?.approval_engine_type
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

// System protocol adapter only. Tree rendering/selection remains shared.
export function createApprovalCatalogAdapter(engine, listChildren) {
  const id = safePickerEngineID(engine.id)
  let searchSequence = 0
  function node(entry) {
    if (entry?.path?.engine_id !== id || entry.path.version !== 'catalog.path/v1' || !Array.isArray(entry.path.segments) ||
        !entry.path.segments.length || !['branch', 'leaf'].includes(entry.role)) throw new Error('invalidCatalogTarget')
    const locator = buildLocator({ engineId: id, path: entry.path.segments.filter(segment => segment.name !== '').map(segment => segment.name), type: entry.term })
    return { id: locator, locator, label: entry.name || engine.name, type: entry.term,
      hasChildren: entry.role === 'branch', metadata: { catalog_entry: structuredClone(entry), has_children: entry.role === 'branch', approval_engine_type: engine.engine_type } }
  }
  async function children(path) {
    const result = await listChildren(String(id), path)
    if (!Array.isArray(result?.nodes)) throw new Error('invalidCatalogTarget')
    return result.nodes.map(node)
  }
  const adapter = {
    listEngines: async () => [{ ...engine, id }],
    getTreeRoot: async () => {
      const roots = await children({ segments: [] })
      if (roots.length !== 1 || !roots[0].hasChildren) throw new Error('invalidCatalogTarget')
      roots[0].children = await children(roots[0].metadata.catalog_entry.path)
      return roots[0]
    },
    getNodeChildren: current => children(current.metadata.catalog_entry.path)
  }
  // Candidate discovery only: no Meta lookup or content read. Do not cache
  // native membership across searches or treat a search result as a Grant.
  adapter.search = async (engineID, keyword, { limit = 20 } = {}) => {
    if (String(engineID) !== String(id) || !Number.isSafeInteger(limit) || limit <= 0) throw new Error('invalidCatalogTarget')
    const query = keyword.trim().toLocaleLowerCase(), sequence = ++searchSequence
    if (!query) return []
    const queue = [await adapter.getTreeRoot()], found = [], visited = new Set()
    for (let index = 0; index < queue.length; index++) {
      if (sequence !== searchSequence) return []
      const current = queue[index], entry = current.metadata.catalog_entry
      const key = JSON.stringify(entry.path)
      if (visited.has(key) || visited.size >= 2000 || entry.path.segments.length > 64) throw new Error('invalidCatalogTarget')
      visited.add(key)
      if (isApprovalSelection(current) && approvalPathLabel(entry.path).toLocaleLowerCase().includes(query)) {
        found.push(current)
        if (found.length >= Math.min(limit, 20)) return found
      }
      if (entry.role === 'branch') {
        const next = current.children || await adapter.getNodeChildren(current)
        if (queue.length + next.length > 2000) throw new Error('invalidCatalogTarget')
        queue.push(...next)
      }
    }
    return found
  }
  return adapter
}
