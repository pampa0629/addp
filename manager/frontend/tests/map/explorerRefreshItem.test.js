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
})
