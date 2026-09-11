import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useExplorerStore } from '../../src/stores/explorer'

const mocks = vi.hoisted(() => ({
  post: vi.fn(),
  getTree: vi.fn(),
  refreshNode: vi.fn(),
  getNodeChildren: vi.fn(),
  getTreeAncestors: vi.fn(),
  searchNodes: vi.fn()
}))

vi.mock('@/api/client', () => ({
  default: {
    post: mocks.post
  }
}))

vi.mock('@/api/dataExplorer', () => ({
  dataExplorerAPI: {
    getTree: mocks.getTree,
    refreshNode: mocks.refreshNode,
    getNodeChildren: mocks.getNodeChildren,
    getTreeAncestors: mocks.getTreeAncestors,
    searchNodes: mocks.searchNodes
  }
}))

describe('explorer refreshItem', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('refreshes the selected item without resetting the engine tree', async () => {
    const store = useExplorerStore()
    const locator = 'addp://engine/9/path/addp/mosaics/bigimages/A49C001003.tif?type=object&item_id=418'
    const tree = {
      locator: 'addp://engine/9',
      children: [
        {
          locator: 'addp://engine/9/path/addp/mosaics/bigimages?type=directory&node_id=153',
          loaded: true,
          children: [
            { locator, label: 'A49C001003.tif' }
          ]
        }
      ]
    }
    store.engineTrees[9] = tree
    store.engineTreeDepths[9] = 4
    const cachedTree = store.engineTrees[9]
    store.selectedLocator = locator
    store.loadPreview = vi.fn().mockResolvedValue({})
    mocks.post.mockResolvedValue({ data: { status: 'success' } })

    const response = await store.refreshItem(locator)

    expect(response).toEqual({ data: { status: 'success' } })
    expect(mocks.post).toHaveBeenCalledWith('/manager/engines/9/items/refresh', null, {
      params: { locator }
    })
    expect(mocks.getTree).not.toHaveBeenCalled()
    expect(store.engineTrees[9]).toBe(cachedTree)
    expect(store.engineTreeDepths[9]).toBe(4)
    expect(store.loadPreview).toHaveBeenCalledWith(locator, 1, '', '', '', '')
  })

  it('reloads the refreshed node children after a basic node scan completes', async () => {
    const store = useExplorerStore()
    const rootLocator = 'addp://engine/12/path/?type=server&node_id=1'
    const bucketLocator = 'addp://engine/12/path/addp?type=bucket&node_id=277'
    const nodeLocator = 'addp://engine/12/path/addp/image?type=prefix&node_id=286'
    const itemLocator = 'addp://engine/12/path/addp/image/new.jpg?type=object&item_id=51955'
    const initialTree = {
      id: rootLocator,
      locator: rootLocator,
      label: 'Business MinIO',
      type: 'server',
      children: [{
        id: bucketLocator,
        locator: bucketLocator,
        label: 'addp',
        type: 'bucket',
        hasChildren: true,
        loaded: true,
        children: [{
          id: nodeLocator,
          locator: nodeLocator,
          label: 'image',
          type: 'prefix',
          metadata: { item_count: 0 },
          hasChildren: true,
          loaded: true,
          children: []
        }]
      }]
    }
    const discoveredItem = {
      id: itemLocator,
      locator: itemLocator,
      label: 'new.jpg',
      type: 'object',
      hasChildren: false
    }

    store.engines = [{ id: 12, name: 'Business MinIO', engine_type: 'minio', connection_status: 'online' }]
    store.engineTrees[12] = initialTree
    store.engineTreeDepths[12] = 1
    store.selectedLocator = nodeLocator
    mocks.refreshNode.mockResolvedValue({ data: { status: 'accepted' } })
    mocks.getNodeChildren.mockResolvedValue({
      id: nodeLocator,
      locator: nodeLocator,
      label: 'image',
      type: 'prefix',
      metadata: { item_count: 1, scanned_at: '2026-09-11T01:02:03Z' },
      hasChildren: true,
      children: [discoveredItem]
    })

    await store.refreshNode(nodeLocator)

    expect(mocks.getTree).not.toHaveBeenCalled()
    expect(mocks.getNodeChildren).toHaveBeenCalledWith(12, nodeLocator)
    expect(store.selectedNode.children.map(node => node.locator)).toEqual([itemLocator])
    expect(store.selectedNode.metadata).toEqual({
      item_count: 1,
      scanned_at: '2026-09-11T01:02:03Z'
    })
  })

  it('clears stale children when the refreshed node is authoritatively empty', async () => {
    const store = useExplorerStore()
    const rootLocator = 'addp://engine/12/path/?type=server&node_id=1'
    const nodeLocator = 'addp://engine/12/path/addp/empty?type=prefix&node_id=300'
    const staleItemLocator = 'addp://engine/12/path/addp/empty/stale.jpg?type=object&item_id=1'

    store.engines = [{ id: 12, name: 'Business MinIO', engine_type: 'minio', connection_status: 'online' }]
    store.engineTrees[12] = {
      id: rootLocator,
      locator: rootLocator,
      label: 'Business MinIO',
      type: 'server',
      children: [{
        id: nodeLocator,
        locator: nodeLocator,
        label: 'empty',
        type: 'prefix',
        metadata: { item_count: 1 },
        hasChildren: true,
        loaded: true,
        children: [{
          id: staleItemLocator,
          locator: staleItemLocator,
          label: 'stale.jpg',
          type: 'object',
          hasChildren: false
        }]
      }]
    }
    store.selectedLocator = nodeLocator
    mocks.refreshNode.mockResolvedValue({ data: { status: 'accepted' } })
    mocks.getNodeChildren.mockResolvedValue({
      id: nodeLocator,
      locator: nodeLocator,
      label: 'empty',
      type: 'prefix',
      metadata: { item_count: 0, scanned_at: '2026-09-11T01:03:03Z' },
      hasChildren: false,
      children: []
    })

    await store.refreshNode(nodeLocator)

    expect(store.selectedNode.children).toEqual([])
    expect(store.selectedNode.hasChildren).toBe(false)
    expect(store.selectedNode.metadata.item_count).toBe(0)
  })
})
