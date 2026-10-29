import { buildLocator, safePickerEngineID, readDataItemIdentityTree, buildDataItemIdentityTree } from '@common-ui'
import { readPermissionTarget } from './directPermission'

export const isApprovalTarget = node => !!node?.metadata?.data_item
export const isApprovalSelection = node => isApprovalTarget(node) || !!node?.metadata?.identity_node

// Nodes expand the currently observed DataItems only. They never become ACLs.
export async function collectApprovalTargets(selection, engineID, adapter, limit = 200) {
  const targets = [], visited = new Set()
  async function visit(node) {
    if (!isApprovalSelection(node) || visited.has(node.id)) throw new Error('invalidCatalogTarget')
    visited.add(node.id)
    if (isApprovalTarget(node)) {
      if (targets.length >= limit) throw new Error('tooManyTargets')
      targets.push(await approvalTargetFromSelection({ raw: { node } }, engineID, adapter))
    } else {
      for (const child of await adapter.getNodeChildren(node)) await visit(child)
    }
  }
  await visit(selection?.raw?.node)
  return targets
}

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
// ResourceTreeSelect owns the UI; exact signing boundaries still come from Meta.
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
        metadata: { engine_id: id, engine_type: engine.engine_type, engine_name: engine.name, has_children: true },
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
    listEngines: async () => [{ ...engine, id }],
    getTreeRoot: root,
    invalidate() { generation++; snapshot = null; nodes = new Map() },
    async getNodeChildren(node) { return (await owned(node)).children },
    async resolveTarget(node) {
      const identity = (await owned(node)).metadata.data_item
      if (!identity) throw new Error('invalidCatalogTarget')
      const { path } = await readPermissionTarget(String(id), identity.item_fingerprint, readFacts)
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
