import { buildLocator } from '../types/resourceLocator.js'

// Shared ResourceTreePicker currently models Engine IDs as safe JS numbers.
// Reject an unrepresentable ID before browsing; never select a rounded target.
export function safePickerEngineID(id) {
  const value = String(id)
  const numeric = Number(value)
  if (!/^[1-9]\d*$/.test(value) || !Number.isSafeInteger(numeric) || String(numeric) !== value) throw new Error('invalidCatalogTarget')
  return numeric
}

// Native catalog browsing only: no metadata lookup, content read or Grant.
export function createEngineCatalogTreeAdapter(engine, listChildren, { isSearchable = () => true } = {}) {
  const id = safePickerEngineID(engine.id)
  let searchSequence = 0
  function node(entry) {
    if (entry?.path?.engine_id !== id || entry.path.version !== 'catalog.path/v1' || !Array.isArray(entry.path.segments) ||
        !entry.path.segments.length || !['branch', 'leaf'].includes(entry.role)) throw new Error('invalidCatalogTarget')
    const locator = buildLocator({ engineId: id, path: entry.path.segments.filter(segment => segment.name !== '').map(segment => segment.name), type: entry.term })
    return { id: locator, locator, label: entry.name || engine.name, type: entry.term,
      hasChildren: entry.role === 'branch', metadata: { catalog_entry: structuredClone(entry), has_children: entry.role === 'branch', engine_type: engine.engine_type, engine_name: engine.name } }
  }
  async function children(path) {
    const result = await listChildren(String(id), path)
    if (!Array.isArray(result)) throw new Error('invalidCatalogTarget')
    return result.map(node)
  }
  const adapter = {
    listEngines: async () => [{ ...engine, id }],
    getTreeRoot: async () => {
      const roots = await children({ segments: [] })
      if (roots.length !== 1 || !roots[0].hasChildren) throw new Error('invalidCatalogTarget')
      roots[0].children = await children(roots[0].metadata.catalog_entry.path)
      return roots[0]
    },
    getNodeChildren: current => {
      const entry = current?.metadata?.catalog_entry
      if (entry?.role !== 'branch' || entry.path?.engine_id !== id) throw new Error('invalidCatalogTarget')
      return children(entry.path)
    }
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
      if (isSearchable(current) && entry.path.segments.filter(segment => segment.name !== '').map(segment => segment.name).join(' / ').toLocaleLowerCase().includes(query)) {
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
