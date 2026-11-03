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
export function isGrantNodePath(path, engineID) {
  if (path?.version !== 'catalog.path/v1' || String(path.engine_id) !== String(engineID) || !Array.isArray(path.segments)) return false
  const [root, ...parts] = path.segments
  if (root?.name !== '' || root.kind !== root.term) return false
  const validName = (name, limit) => typeof name === 'string' && name.length > 0 && new TextEncoder().encode(name).length <= limit && new TextDecoder().decode(new TextEncoder().encode(name)) === name && !name.includes('\0')
  if (root.term === 'server') return parts.length <= 1 && parts.every(part => part.term === 'schema' && part.kind === 'namespace' && validName(part.name, 512))
  return root.term === 'root' && parts.length <= 63 && parts.every(part => part.term === 'directory' && part.kind === 'directory' && validName(part.name, 4096) && !['.', '..'].includes(part.name) && !/[\\/]/.test(part.name))
}
export const isApprovalNode = node => !!node?.metadata?.node_identity && isGrantNodePath(node.metadata.node_identity.target, node.metadata.engine_id)

export function createApprovalNodeCatalogAdapter(engine, readPage) {
  if (!['postgresql', 'nfs'].includes(engine.engine_type)) throw new Error('invalidCatalogTarget')
  const id = safePickerEngineID(engine.id), term = engine.engine_type === 'postgresql' ? 'server' : 'root'
  const rootPath = { version: 'catalog.path/v1', engine_id: id, segments: [{ term, kind: term, name: '' }] }
  let epoch = 0, rootPromise, nodes = new Map(), childrenPromises = new Map()
  const pathKey = path => serializeEngineCatalogTarget({ ...path, engine_id: String(id) })
  const make = (fact, hasChildren) => {
    if (fact?.exists !== true || !isGrantNodePath(fact.target, id) || fact.target.segments[0].term !== term) throw new Error('invalidCatalogTarget')
    const path = structuredClone(fact.target), root = path.segments.length === 1
    const type = root ? 'root' : term === 'server' ? 'schema' : 'dir'
    const locator = buildLocator({ engineId: id, path: path.segments.slice(1).map(part => part.name), type })
    const node = { id: pathKey(path), locator, label: root ? engine.name : path.segments.at(-1).name, type, hasChildren,
      metadata: { engine_id: id, engine_type: engine.engine_type, engine_name: engine.name, has_children: hasChildren, node_identity: { target: path, exists: true } } }
    nodes.set(node.id, node); return node
  }
  async function page(target, number, captured) {
    const result = await readPage(String(id), { target, page: number, page_size: 100 })
    if (captured !== epoch || !approvalTargetsEqual(result?.parent?.target, target) || result.parent.exists !== true || result.page !== number || result.page_size !== 100 || !Array.isArray(result.nodes) || !Number.isSafeInteger(result.total) || result.total < 0 || result.nodes.length !== Math.max(0, Math.min(100, result.total - (number - 1) * 100))) throw new Error('invalidCatalogTarget')
    return result
  }
  async function root() {
    if (!rootPromise) { const captured = epoch; rootPromise = page(rootPath, 1, captured).then(first => ({ node: make(first.parent, first.total > 0), first })).catch(error => { if (captured === epoch) rootPromise = undefined; throw error }) }
    return (await rootPromise).node
  }
  async function children(node) {
    const target = node.metadata.node_identity.target, key = pathKey(target)
    if (term === 'server' && target.segments.length > 1) return []
    if (!childrenPromises.has(key)) {
      const captured = epoch
      const promise = (async () => {
        const first = target.segments.length === 1 ? (await rootPromise).first : await page(target, 1, captured)
        const result = [], seen = new Set()
        if (first.total > 20000) throw new Error('permissionTreeCapacity')
        for (let number = 1; result.length < first.total; number++) {
          const current = number === 1 ? first : await page(target, number, captured)
          if (current.total !== first.total) throw new Error('invalidCatalogTarget')
          for (const fact of current.nodes) {
            if (!isGrantNodePath(fact.target, id) || fact.target.segments.length !== target.segments.length + 1 || !target.segments.every((part, i) => ['term', 'kind', 'name'].every(k => fact.target.segments[i][k] === part[k])) || fact.exists !== true) throw new Error('invalidCatalogTarget')
            const childKey = pathKey(fact.target); if (seen.has(childKey)) throw new Error('invalidCatalogTarget'); seen.add(childKey)
            result.push(make(fact, term === 'root'))
          }
        }
        return result
      })().catch(error => { if (captured === epoch) childrenPromises.delete(key); throw error })
      childrenPromises.set(key, promise)
    }
    return childrenPromises.get(key)
  }
  return {
    listEngines: async () => [{ ...engine, id }], getTreeRoot: root,
    invalidate() { epoch++; rootPromise = undefined; childrenPromises = new Map(); nodes = new Map() },
    async getNodeChildren(node) { const owned = nodes.get(node?.id); if (!owned || owned.locator !== node.locator) throw new Error('invalidCatalogTarget'); return children(owned) },
    async resolveTarget(node) { const owned = nodes.get(node?.id); if (!owned || owned.locator !== node.locator || !isApprovalNode(owned)) throw new Error('invalidCatalogTarget'); return structuredClone(owned.metadata.node_identity.target) },
    async search(engineID, keyword, { limit = 20 } = {}) {
      if (String(engineID) !== String(id) || !Number.isSafeInteger(limit) || limit <= 0) throw new Error('invalidCatalogTarget')
      const query = keyword.trim().toLocaleLowerCase(); if (!query) return []
      await children(await root())
      return [...nodes.values()].filter(node => node.label.toLocaleLowerCase().includes(query)).slice(0, Math.min(limit, 20))
    }
  }
}

// One selectable tree, two owner facts. Nodes stay available independently of
// the bounded DataItem snapshot, which is fetched only on item browsing/search.
export function createMixedApprovalCatalogAdapter(engine, listItems, readFacts, readNodes) {
  const items = createApprovalCatalogAdapter(engine, listItems, readFacts)
  if (!['postgresql', 'nfs'].includes(engine.engine_type)) return items
  const scopes = createApprovalNodeCatalogAdapter(engine, readNodes)
  let generation = 0
  const display = node => ({ ...node, hasChildren: true, metadata: { ...node.metadata, has_children: true } })
  const current = (epoch, value) => {
    if (epoch !== generation) throw new Error('stalePermissionTree')
    return value
  }
  return {
    listEngines: items.listEngines,
    async getTreeRoot() { const epoch = generation; return current(epoch, display(await scopes.getTreeRoot())) },
    invalidate() { generation++; items.invalidate(); scopes.invalidate() },
    async getNodeChildren(node) {
      const epoch = generation
      if (!isApprovalNode(node)) return []
      const path = await scopes.resolveTarget(node)
      const branches = await scopes.getNodeChildren(node)
      if (engine.engine_type === 'postgresql' && path.segments.length === 1) return current(epoch, branches.map(display))
      let branch = await items.getTreeRoot()
      for (const segment of path.segments.slice(1)) branch = branch?.children.find(child => !isApprovalTarget(child) && child.label === segment.name)
      const leaves = branch ? (await items.getNodeChildren(branch)).filter(isApprovalTarget) : []
      return current(epoch, [...branches.map(display), ...leaves])
    },
    resolveTarget(node) { return isApprovalNode(node) ? scopes.resolveTarget(node) : items.resolveTarget(node) },
    async search(engineID, keyword, options = {}) {
      const epoch = generation
      const nodes = await scopes.search(engineID, keyword, options)
      const leaves = (await items.search(engineID, keyword, options)).filter(isApprovalTarget)
      return current(epoch, [...nodes.map(display), ...leaves].slice(0, Math.min(options.limit ?? 20, 20)))
    }
  }
}

export function approvalScopeContains(scope, target, recursive, targetIsNode = isGrantNodePath(target, target?.engine_id)) {
  if (!isGrantNodePath(scope, scope?.engine_id) || !target || String(scope.engine_id) !== String(target.engine_id) || scope.version !== target.version) return false
  const prefix = scope.segments.every((part, index) => ['term', 'kind', 'name'].every(key => part[key] === target.segments?.[index]?.[key]))
  return prefix && target.segments.length > scope.segments.length && (recursive || (target.segments.length === scope.segments.length + 1 && !targetIsNode))
}
