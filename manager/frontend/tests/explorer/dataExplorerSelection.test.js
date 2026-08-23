import { describe, expect, it, vi } from 'vitest'

import { resolveCanonicalNodeSelection } from '../../src/utils/dataExplorerSelection.js'

describe('data explorer node selection', () => {
  it('resolves a synthetic engine root before it can be selected or routed', async () => {
    const syntheticNode = {
      id: 'addp://engine/11/path/?type=server',
      locator: 'addp://engine/11/path/?type=server',
      engineId: 11,
      type: 'server',
      hasChildren: true,
      loaded: false,
    }
    const canonicalNode = {
      ...syntheticNode,
      id: 'addp://engine/11/path/?type=server&node_id=267',
      locator: 'addp://engine/11/path/?type=server&node_id=267',
      loaded: true,
    }
    const loadTree = vi.fn().mockResolvedValue(canonicalNode)

    await expect(resolveCanonicalNodeSelection({
      node: syntheticNode,
      locator: syntheticNode.locator,
      loadTree,
    })).resolves.toEqual({
      node: canonicalNode,
      locator: canonicalNode.locator,
    })
    expect(loadTree).toHaveBeenCalledTimes(1)
    expect(loadTree).toHaveBeenCalledWith(11)
  })

  it('does not reload a canonical engine root that already carries node_id', async () => {
    const canonicalNode = {
      id: 'addp://engine/11/path/?type=server&node_id=267',
      locator: 'addp://engine/11/path/?type=server&node_id=267',
      engineId: 11,
      type: 'server',
      hasChildren: true,
      loaded: false,
    }
    const loadTree = vi.fn()

    await expect(resolveCanonicalNodeSelection({
      node: canonicalNode,
      locator: canonicalNode.locator,
      loadTree,
    })).resolves.toEqual({
      node: canonicalNode,
      locator: canonicalNode.locator,
    })
    expect(loadTree).not.toHaveBeenCalled()
  })
})
