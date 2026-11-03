import { buildLocator, safePickerEngineID, readDataItemIdentityTree, buildDataItemIdentityTree, serializeEngineCatalogTarget } from '@common-ui'
import { readNativeApprovalFacts } from '@common-ui'

export const isApprovalTarget = node => !!node?.metadata?.data_item
export const isApprovalSelection = node => isApprovalTarget(node) || !!node?.metadata?.identity_node

export async function approvalTargetFromSelection(selection, engineID, adapter) {
  const node = selection?.raw?.node
  if (!isApprovalTarget(node) || String(node.metadata.data_item.engine_id) !== String(engineID)) throw new Error('invalidCatalogTarget')
  return adapter.resolveTarget(node)
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

// System's adapter consumes the identity-only Meta projection through System.
// Shared resource-tree components own the UI; exact signing boundaries still come from Meta.
export function createApprovalCatalogAdapter(engine, listItems, readFacts) {
  const id = safePickerEngineID(engine.id)
  let snapshot, generation = 0, nodes = new Map()
  function convert(current, index, parents = []) {
    const path = current.item ? current.item.ancestors.slice(1).map(part => part.name) : [...parents, current.label]
    if (current.item && path.join('/') !== current.item.full_name) path.push(current.item.name)
    const children = current.children || []
    // A whole container's scope node and its one DataItem have one display row.
    if (!current.item && children.length === 1 && children[0].item?.full_name === path.join('/')) return convert(children[0], index, parents)
    const locator = buildLocator({ engineId: id, path, type: current.type })
    const node = { id: current.id, locator, label: current.label, type: current.type, hasChildren: !current.item,
      metadata: { engine_id: id, engine_type: engine.engine_type, engine_name: engine.name, has_children: !current.item,
        path: current.item?.full_name || path.join('/'),
        ...(current.item ? { data_item: current.item } : { identity_node: current.id }) } }
    node.children = children.map(child => convert(child, index, path))
    index.set(node.id, node)
    return node
  }
  async function root() {
    if (!snapshot) {
      const epoch = generation, index = new Map()
      snapshot = (async () => {
      const rows = await readDataItemIdentityTree(String(id), listItems, () => epoch === generation)
      const roots = buildDataItemIdentityTree(rows)
      if (roots.length > 1) throw new Error('invalidCatalogTarget')
      const result = { id: `engine:${id}`, label: engine.name, type: 'root', locator: buildLocator({ engineId: id, path: [], type: 'root' }), hasChildren: true,
        metadata: { engine_id: id, engine_type: engine.engine_type, engine_name: engine.name, has_children: true, identity_node: `engine:${id}` },
        children: (roots[0]?.children || []).map(node => convert(node, index)) }
      index.set(result.id, result)
      nodes = index
      return result
    })().catch(error => { if (epoch === generation) { snapshot = null; nodes = new Map() }; throw error })
    }
    return snapshot
  }
  async function owned(node) {
    await root()
    const current = nodes.get(node?.id)
    if (!current || current.locator !== node.locator || current.metadata.data_item?.item_fingerprint !== node.metadata?.data_item?.item_fingerprint) throw new Error('invalidCatalogTarget')
    return current
  }
  return {
    completeTree: true,
    listEngines: async () => [{ ...engine, id }],
    getTreeRoot: root,
    invalidate() { generation++; snapshot = null; nodes = new Map() },
    async getNodeChildren(node) { return (await owned(node)).children },
    async resolveTarget(node) {
      const identity = (await owned(node)).metadata.data_item
      if (!identity) throw new Error('invalidCatalogTarget')
      const { path } = await readAuthorizationTarget(String(id), identity.item_fingerprint, readFacts)
      return { ...path, engine_id: String(id), segments: path.segments.map(part => ({ ...part })) }
    },
    async search(engineID, keyword, { limit = 20 } = {}) {
      if (String(engineID) !== String(id) || !Number.isSafeInteger(limit) || limit <= 0) throw new Error('invalidCatalogTarget')
      await root()
      const query = keyword.trim().toLocaleLowerCase()
      if (!query) return []
      return [...nodes.values()].filter(node => isApprovalSelection(node) &&
        (node.metadata.data_item?.full_name || node.label).toLocaleLowerCase().includes(query)).slice(0, Math.min(limit, 20))
    }
  }
}

export async function readAuthorizationTarget(engineID, fingerprint, readFacts) {
  const facts = await readFacts(engineID, fingerprint)
  const target = facts?.boundary?.metadata_target?.target || facts?.boundary?.scope?.root || facts?.boundary?.native_target || facts?.boundary?.file_composition?.refs?.find(ref => ref.primary === true)?.target
  if (String(target?.engine_id) !== String(engineID) || facts?.item_fingerprint !== fingerprint) throw new Error('invalidDataItemBoundary')
  const observed = await readNativeApprovalFacts(target, async (id, fp) => {
    if (String(id) !== String(engineID) || fp !== fingerprint) throw new Error('invalidDataItemBoundary')
    return facts
  })
  if (observed.state !== 'complete') throw new Error('invalidDataItemBoundary')
  return { path: target, binding: observed.binding }
}


// Nodes have their own owner projection. Never derive this scope from a loaded
// list of current DataItems or use native directory browsing as its identity.
export function isPGGrantNodePath(path, engineID) {
 return path?.version === 'catalog.path/v1' && String(path.engine_id) === String(engineID) &&
  Array.isArray(path.segments) && [1, 2].includes(path.segments.length) &&
  path.segments[0]?.term === 'server' && path.segments[0]?.kind === 'server' && path.segments[0]?.name === '' &&
  (path.segments.length === 1 || (path.segments[1]?.term === 'schema' && path.segments[1]?.kind === 'namespace' && typeof path.segments[1]?.name === 'string' && path.segments[1].name.length > 0 && new TextEncoder().encode(path.segments[1].name).length <= 512 && new TextDecoder().decode(new TextEncoder().encode(path.segments[1].name)) === path.segments[1].name && !path.segments[1].name.includes('\0')))
}
export const isApprovalNode = node => !!node?.metadata?.node_identity && isPGGrantNodePath(node.metadata.node_identity.target, node.metadata.engine_id)

export function createApprovalNodeCatalogAdapter(engine, readPage) {
 if (engine.engine_type !== 'postgresql') throw new Error('invalidCatalogTarget')
 const id = safePickerEngineID(engine.id), rootPath = { version: 'catalog.path/v1', engine_id: id, segments: [{ term: 'server', kind: 'server', name: '' }] }
 let epoch = 0, rootPromise, nodes = new Map(), childPromise
 const pathKey = path => serializeEngineCatalogTarget({ ...path, engine_id: String(id) })
 const make = (fact, hasChildren) => {
  if (fact?.exists !== true || !isPGGrantNodePath(fact.target, id)) throw new Error('invalidCatalogTarget')
  const path = structuredClone(fact.target), root = path.segments.length === 1
  const locator = buildLocator({ engineId: id, path: path.segments.filter(part => part.name !== '').map(part => part.name), type: root ? 'root' : 'schema' })
  const node = { id: pathKey(path), locator, label: root ? engine.name : path.segments[1].name, type: root ? 'root' : 'schema', hasChildren,
   metadata: { engine_id: id, engine_type: engine.engine_type, engine_name: engine.name, has_children: hasChildren, node_identity: { target: path, exists: true } } }
  nodes.set(node.id, node); return node
 }
 async function page(target, number, captured) {
  const result = await readPage(String(id), { target, page: number, page_size: 100 })
  if (captured !== epoch || !approvalTargetsEqual(result?.parent?.target, target) || result.parent.exists !== true ||
   result.page !== number || result.page_size !== 100 || !Array.isArray(result.nodes) || !Number.isSafeInteger(result.total) || result.total < 0 ||
   result.nodes.length !== Math.max(0, Math.min(100, result.total - (number - 1) * 100))) throw new Error('invalidCatalogTarget')
  return result
 }
 async function root() {
  if (!rootPromise) { const captured = epoch; rootPromise = page(rootPath, 1, captured).then(first => ({ node: make(first.parent, first.total > 0), first })).catch(error => { if (captured === epoch) rootPromise = undefined; throw error }) }
  return (await rootPromise).node
 }
 async function children() {
  if (!childPromise) { const captured = epoch; childPromise = (async () => {
   await root(); const first = (await rootPromise).first, result = [], seen = new Set()
   if (first.total > 20000) throw new Error('permissionTreeCapacity')
   for (let number = 1; result.length < first.total; number++) {
    const current = number === 1 ? first : await page(rootPath, number, captured)
    if (current.total !== first.total) throw new Error('invalidCatalogTarget')
    for (const fact of current.nodes) {
     if (!isPGGrantNodePath(fact.target, id) || fact.target.segments.length !== 2 || fact.exists !== true) throw new Error('invalidCatalogTarget')
     const key = pathKey(fact.target); if (seen.has(key)) throw new Error('invalidCatalogTarget'); seen.add(key)
     result.push(make(fact, false))
    }
   }
   return result
  })().catch(error => { if (captured === epoch) childPromise = undefined; throw error }) }
  return childPromise
 }
 return {
  listEngines: async () => [{ ...engine, id }], getTreeRoot: root,
  invalidate() { epoch++; rootPromise = childPromise = undefined; nodes = new Map() },
  async getNodeChildren(node) { const owned = nodes.get(node?.id); if (!owned || owned.locator !== node.locator) throw new Error('invalidCatalogTarget'); return owned.type === 'root' ? children() : [] },
  async resolveTarget(node) { const owned = nodes.get(node?.id); if (!owned || owned.locator !== node.locator || !isApprovalNode(owned)) throw new Error('invalidCatalogTarget'); return structuredClone(owned.metadata.node_identity.target) },
  async search(engineID, keyword, { limit = 20 } = {}) { if (String(engineID) !== String(id) || !Number.isSafeInteger(limit) || limit <= 0) throw new Error('invalidCatalogTarget'); const query = keyword.trim().toLocaleLowerCase(); if (!query) return []; return [await root(), ...await children()].filter(node => node.label.toLocaleLowerCase().includes(query)).slice(0, Math.min(limit, 20)) }
 }
}
