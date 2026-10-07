import { buildLocator } from '@common-ui/types/resourceLocator'

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

export function approvalTargetFromSelection(selection, engineID) {
  const entry = selection?.raw?.node?.metadata?.catalog_entry
  const id = safePickerEngineID(engineID)
  if (!isApprovalTable(selection?.raw?.node) || entry.path?.engine_id !== id || !Array.isArray(entry.path?.segments) || entry.path.segments.length < 2) {
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
  function node(entry) {
    if (entry?.path?.engine_id !== id || entry.path.version !== 'catalog.path/v1' || !Array.isArray(entry.path.segments) ||
        !entry.path.segments.length || !['branch', 'leaf'].includes(entry.role)) throw new Error('invalidCatalogTarget')
    const locator = buildLocator({ engineId: id, path: entry.path.segments.filter(segment => segment.name !== '').map(segment => segment.name), type: entry.term })
    return { id: locator, locator, label: entry.name || engine.name, type: entry.term,
      hasChildren: entry.role === 'branch', metadata: { catalog_entry: structuredClone(entry), has_children: entry.role === 'branch' } }
  }
  async function children(path) {
    const result = await listChildren(String(id), path)
    if (!Array.isArray(result?.nodes)) throw new Error('invalidCatalogTarget')
    return result.nodes.map(node)
  }
  return {
    listEngines: async () => [{ ...engine, id }],
    getTreeRoot: async () => {
      const roots = await children({ segments: [] })
      if (roots.length !== 1 || !roots[0].hasChildren) throw new Error('invalidCatalogTarget')
      roots[0].children = await children(roots[0].metadata.catalog_entry.path)
      return roots[0]
    },
    getNodeChildren: current => children(current.metadata.catalog_entry.path)
  }
}
