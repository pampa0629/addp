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
    mocks.getNodeChildren.mockResolvedValue({
      parent_locator: schemaLocator,
      children: schemaChildren
    })

    const store = useExplorerStore()
    store.engines = [{ id: 2, name: 'Business PostgreSQL', engine_type: 'postgresql', connection_status: 'online' }]

    const revealed = await store.revealLocator(targetLocator)

    expect(mocks.getTree).toHaveBeenCalledWith(2, 1)
    expect(mocks.getNodeChildren).toHaveBeenCalledTimes(1)
    expect(mocks.getNodeChildren).toHaveBeenCalledWith(2, schemaLocator)
    expect(store.engineTrees[2].children[0].children.map(node => node.locator)).toEqual([
      siblingLocator,
      targetLocator
    ])
    expect(revealed.node.locator).toBe(targetLocator)
    expect(store.selectedLocator).toBe(targetLocator)
  })
})
