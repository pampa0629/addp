import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useExplorerStore } from '../../src/stores/explorer'

const mocks = vi.hoisted(() => ({
  getTree: vi.fn(),
  getNodeChildren: vi.fn(),
  getTreeAncestors: vi.fn(),
  refreshNode: vi.fn(),
  searchNodes: vi.fn()
}))

vi.mock('@/api/client', () => ({
  default: {}
}))

vi.mock('@/api/dataExplorer', () => ({
  dataExplorerAPI: {
    getTree: mocks.getTree,
    getNodeChildren: mocks.getNodeChildren,
    getTreeAncestors: mocks.getTreeAncestors,
    refreshNode: mocks.refreshNode,
    searchNodes: mocks.searchNodes
  }
}))

describe('explorer revealLocator', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('keeps a newer node reload when an older child response arrives last', async () => {
    const store = useExplorerStore()
    const root = 'addp://engine/25/path/?type=server&node_id=250'
    const schema = 'addp://engine/25/path/business?type=schema&node_id=251'
    const node = (locator, label, children = []) => ({ id: locator, locator, label, type: 'schema', hasChildren: true, children })
    store.engineTrees[25] = node(root, 'root', [node(schema, 'business')])
    let resolveOld
    mocks.getNodeChildren.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    const first = store.loadNodeChildren(schema)
    await vi.waitFor(() => expect(resolveOld).toBeTypeOf('function'))
    mocks.getTreeAncestors.mockResolvedValue({ ancestors: [node(root, 'root'), node(schema, 'business')], target_locator: schema })
    const fresh = node('addp://engine/25/path/business/new?type=table&item_id=253', 'new')
    mocks.getNodeChildren.mockResolvedValueOnce(node(schema, 'business', [fresh]))
    await store.loadNodeChildren(schema, true)
    resolveOld(node(schema, 'business', [node('addp://engine/25/path/business/old?type=table&item_id=252', 'old')]))
    await first
    expect(store.engineTrees[25].children[0].children.map(child => child.label)).toEqual(['new'])
    expect(store.nodeChildrenCache[schema].children.map(child => child.label)).toEqual(['new'])
  })

  it('shares concurrent ordinary child loads and applies cached children to the current tree', async () => {
    const store = useExplorerStore()
    const locator = 'addp://engine/25/path/?type=server&node_id=250'
    store.engineTrees[25] = { id: locator, locator, type: 'server', children: [] }
    let resolveChildren
    const pending = new Promise(resolve => { resolveChildren = resolve })
    mocks.getNodeChildren.mockReturnValue(pending)
    const first = store.loadNodeChildren(locator)
    const second = store.loadNodeChildren(locator)
    await vi.waitFor(() => expect(resolveChildren).toBeTypeOf('function'))
    const child = { id: 'child', locator: 'child', type: 'table', label: 'child', children: [] }
    resolveChildren({ locator, children: [child] })
    await Promise.all([first, second])
    expect(mocks.getNodeChildren).toHaveBeenCalledTimes(1)
    store.engineTrees[25] = { id: locator, locator, type: 'server', children: [] }
    await store.loadNodeChildren(locator)
    expect(mocks.getNodeChildren).toHaveBeenCalledTimes(1)
    expect(store.engineTrees[25].children.map(child => child.label)).toEqual(['child'])
  })

  it('does not let a delayed locator reveal replace a newer selected resource', async () => {
    const store = useExplorerStore()
    const root = 'addp://engine/25/path/?type=server&node_id=250'
    const target = 'addp://engine/25/path/business?type=schema&node_id=251'
    const later = 'addp://engine/29/path/?type=server&node_id=290'
    const node = locator => ({ id: locator, locator, label: locator, type: 'schema', hasChildren: true, children: [] })
    store.engineTrees[25] = node(root)
    let resolveAncestors
    mocks.getTreeAncestors.mockImplementationOnce(() => new Promise(resolve => { resolveAncestors = resolve }))
    mocks.getNodeChildren.mockResolvedValue({ children: [node(target)] })
    const pending = store.revealLocator(target)
    await vi.waitFor(() => expect(resolveAncestors).toBeTypeOf('function'))
    store.selectNodeContext(node(later), later)
    resolveAncestors({ ancestors: [node(root), node(target)], target_locator: target })
    await pending
    expect(store.selectedLocator).toBe(later)
    expect([...store.expandedLocators]).toEqual([])
  })

  it('discards a failed locator reveal after the user has selected another resource', async () => {
    const store = useExplorerStore()
    const root = 'addp://engine/25/path/?type=server&node_id=250'
    const target = 'addp://engine/25/path/business?type=schema&node_id=251'
    store.engineTrees[25] = { id: root, locator: root, type: 'server', children: [] }
    let rejectAncestors
    mocks.getTreeAncestors.mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectAncestors = reject }))
    const pending = store.revealLocator(target)
    await vi.waitFor(() => expect(rejectAncestors).toBeTypeOf('function'))
    store.selectNode('later-selection')
    rejectAncestors(new Error('stale failure'))
    await expect(pending).resolves.toBeNull()
    expect(store.selectedLocator).toBe('later-selection')
  })

  it('never lets an older deeper root response replace a newer root snapshot', async () => {
    const store = useExplorerStore()
    let resolveOld
    mocks.getTree.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    const first = store.loadTree(25, 2)
    const root = 'addp://engine/25/path/?type=server&node_id=250'
    mocks.getTree.mockResolvedValueOnce({ id: root, locator: root, type: 'server', label: 'new', children: [] })
    await store.loadTree(25, 1, true)
    resolveOld({ id: root, locator: root, type: 'server', label: 'old', children: [] })
    await first
    expect(store.engineTrees[25].label).toBe('new')
    expect(store.engineTreeDepths[25]).toBe(1)
  })

  it('collapses only the exact subtree, preserving similarly named siblings and other engines', () => {
    const store = useExplorerStore()
    const root = 'addp://engine/12/path/?type=service&node_id=1'
    const folder = 'addp://engine/12/path/data?type=prefix&node_id=2'
    const child = 'addp://engine/12/path/data/child?type=prefix&node_id=3'
    const sibling = 'addp://engine/12/path/database?type=prefix&node_id=4'
    const other = 'addp://engine/13/path/data?type=prefix&node_id=5'
    store.expandedLocators = new Set([root, folder, child, sibling, other])
    store.collapseNode(folder)
    expect([...store.expandedLocators]).toEqual([root, sibling, other])
    store.collapseNode(root)
    expect([...store.expandedLocators]).toEqual([other])
  })

  it('reloads authoritative ancestor statistics after upload without losing siblings or selection', async () => {
    const store = useExplorerStore()
    const root = 'addp://engine/12/path/?type=service&node_id=268'
    const bucket = 'addp://engine/12/path/addp?type=bucket&node_id=277'
    const doc = 'addp://engine/12/path/addp/doc?type=prefix&node_id=290'
    const sibling = 'addp://engine/12/path/addp/images?type=prefix&node_id=291'
    const file = 'addp://engine/12/path/addp/doc/new.md?type=object&item_id=500'
    const node = (locator, type, count, children = []) => ({
      id: locator, locator, type, loaded: true, hasChildren: true, children,
      metadata: { item_count: count, total_size_bytes: count * 10, scanned_at: '2026-09-15T10:39:00Z' }
    })
    store.engineTrees[12] = node(root, 'service', 20, [
      node(bucket, 'bucket', 15, [node(doc, 'prefix', 1), node(sibling, 'prefix', 14)])
    ])
    store.selectedLocator = doc
    store.expandedLocators = new Set([root, bucket, doc, sibling])
    for (const locator of [root, bucket, doc, sibling]) {
      store.nodeChildrenCache[locator] = { children: [], timestamp: Date.now() }
    }
    mocks.getTreeAncestors.mockResolvedValue({ ancestors: [
      node(root, 'service', 21), node(bucket, 'bucket', 16), node(doc, 'prefix', 2)
    ] })
    mocks.getNodeChildren.mockResolvedValue(node(doc, 'prefix', 2, [{ locator: file, type: 'object' }]))

    await store.loadNodeChildren(doc, true)

    expect(mocks.getTreeAncestors).toHaveBeenCalledWith(12, doc)
    expect(mocks.getTree).not.toHaveBeenCalled()
    expect(mocks.refreshNode).not.toHaveBeenCalled()
    expect(store.engineTrees[12].metadata.item_count).toBe(21)
    expect(store.engineTrees[12].children[0].metadata.item_count).toBe(16)
    expect(store.engineTrees[12].children[0].children[1].locator).toBe(sibling)
    expect(store.selectedNode.metadata).toEqual(node(doc, 'prefix', 2).metadata)
    expect(store.selectedNode.children.map(child => child.locator)).toEqual([file])
    expect([...store.expandedLocators]).toEqual([root, bucket, doc, sibling])
    expect(store.nodeChildrenCache[root]).toBeUndefined()
    expect(store.nodeChildrenCache[bucket]).toBeUndefined()
    expect(store.nodeChildrenCache[sibling]).toBeDefined()
    expect(store.nodeChildrenCache[doc].children[0].locator).toBe(file)
  })

  it('loads sibling items for expanded ancestors when restoring a deep locator', async () => {
    const rootLocator = 'addp://engine/2/path/?type=server&node_id=10'
    const schemaLocator = 'addp://engine/2/path/outdoor?type=schema&node_id=11'
    const targetLocator = 'addp://engine/2/path/outdoor/ods_outdoor_persons?type=table&item_id=52899'
    const siblingLocator = 'addp://engine/2/path/outdoor/ods_outdoor_events?type=table&item_id=52898'
    const root = {
      id: rootLocator,
      locator: rootLocator,
      label: 'Business PostgreSQL',
      type: 'server',
      hasChildren: true,
      children: [{
        id: schemaLocator,
        locator: schemaLocator,
        label: 'outdoor',
        type: 'schema',
        hasChildren: true,
        loaded: false,
        children: []
      }]
    }
    const chain = [
      { id: rootLocator, locator: rootLocator, label: 'Business PostgreSQL', type: 'server', hasChildren: true },
      { id: schemaLocator, locator: schemaLocator, label: 'outdoor', type: 'schema', hasChildren: true },
      { id: targetLocator, locator: targetLocator, label: 'ods_outdoor_persons', type: 'table', hasChildren: false }
    ]
    const schemaChildren = [
      { id: siblingLocator, locator: siblingLocator, label: 'ods_outdoor_events', type: 'table', hasChildren: false },
      { id: targetLocator, locator: targetLocator, label: 'ods_outdoor_persons', type: 'table', hasChildren: false }
    ]

    mocks.getTree.mockResolvedValue(root)
    mocks.getTreeAncestors.mockResolvedValue({
      target_locator: targetLocator,
      ancestors: chain
    })
    mocks.getNodeChildren.mockImplementation(async (_engineId, locator) => locator === rootLocator
      ? { ...root, children: [...root.children] }
      : { parent_locator: schemaLocator, children: schemaChildren })

    const store = useExplorerStore()
    store.engines = [{ id: 2, name: 'Business PostgreSQL', engine_type: 'postgresql', connection_status: 'online' }]

    const revealed = await store.revealLocator(targetLocator)

    expect(mocks.getTree).toHaveBeenCalledWith(2, 1)
    expect(mocks.getNodeChildren).toHaveBeenCalledTimes(2)
    expect(mocks.getNodeChildren).toHaveBeenCalledWith(2, rootLocator)
    expect(mocks.getNodeChildren).toHaveBeenCalledWith(2, schemaLocator)
    expect(store.engineTrees[2].children[0].children.map(node => node.locator)).toEqual([
      siblingLocator,
      targetLocator
    ])
    expect(revealed.node.locator).toBe(targetLocator)
    expect(store.selectedLocator).toBe(targetLocator)
  })

  it('restores one Redis keyspace dataset without registering content keys', async () => {
    const rootLocator = 'addp://engine/24/path/?type=server&node_id=10'
    const locator = 'addp://engine/24/path/keyspace?type=keyspace&item_id=900'
    const keys = [{ id: locator, locator, label: 'keyspace', type: 'keyspace', hasChildren: false, children: [] }]
    const root = { id: rootLocator, locator: rootLocator, label: 'Redis', type: 'server',
      hasChildren: true, children: [] }
    const target = keys.at(-1)
    mocks.getTree.mockResolvedValue(root)
    mocks.getTreeAncestors.mockResolvedValue({ target_locator: target.locator, ancestors: [root, target] })
    mocks.getNodeChildren.mockResolvedValue({ ...root, children: keys })
    const store = useExplorerStore()
    store.engines = [{ id: 24, name: 'Redis', engine_type: 'redis', connection_status: 'online' }]

    const revealed = await store.revealLocator(target.locator)

    expect(store.engineTrees[24].children.map(node => node.locator)).toEqual(keys.map(node => node.locator))
    expect(mocks.getNodeChildren).toHaveBeenCalledWith(24, rootLocator)
    expect([...store.expandedLocators]).toContain(rootLocator)
    expect(revealed.node.locator).toBe(target.locator)
    expect(store.selectedLocator).toBe(target.locator)
    expect(mocks.refreshNode).not.toHaveBeenCalled()
  })

  it('preserves loaded descendant siblings when filling the root child list', async () => {
    const store = useExplorerStore()
    const root = 'addp://engine/2/path/?type=server&node_id=10'
    const schema = 'addp://engine/2/path/public?type=schema&node_id=11'
    const other = 'addp://engine/2/path/other?type=schema&node_id=12'
    const item = name => ({ locator: `addp://engine/2/path/${name === 'other' ? 'other' : 'public'}/${name}?type=table&item_id=${{ target: 1, sibling: 2, other: 3 }[name]}`,
      type: 'table', label: name, hasChildren: false, children: [] })
    const target = item('target')
    const siblings = [target, item('sibling')]
    const node = (locator, type, children = []) => ({ id: locator, locator, type, hasChildren: true, children, loaded: true })
    store.engines = [{ id: 2, name: 'PostgreSQL', engine_type: 'postgresql', connection_status: 'online' }]
    store.engineTrees[2] = node(root, 'server', [node(schema, 'schema', siblings), node(other, 'schema', [item('other')])])
    store.nodeChildrenCache[schema] = { children: siblings, timestamp: Date.now() }
    mocks.getTreeAncestors.mockResolvedValue({ target_locator: target.locator,
      ancestors: [node(root, 'server'), node(schema, 'schema'), target] })
    mocks.getNodeChildren.mockResolvedValue({ ...node(root, 'server'),
      children: [{ ...node(schema, 'schema'), loaded: false }, { ...node(other, 'schema'), loaded: false }] })

    await store.revealLocator(target.locator)

    expect(store.engineTrees[2].children[0].children.map(child => child.locator)).toEqual(siblings.map(child => child.locator))
    expect(store.engineTrees[2].children[1].children).toHaveLength(1)
    expect(store.selectedNode.locator).toBe(target.locator)
    expect(mocks.getNodeChildren).toHaveBeenCalledTimes(1)
    expect(mocks.refreshNode).not.toHaveBeenCalled()
  })
})
