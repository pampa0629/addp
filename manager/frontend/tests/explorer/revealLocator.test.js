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
