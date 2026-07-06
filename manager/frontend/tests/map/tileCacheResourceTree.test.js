import { describe, expect, it } from 'vitest'
import { catalogRootTypeForEngine } from '@addp/common-frontend'
import {
  createResourceRootNode,
  mergeAncestorChainIntoResourceTree,
  tableSelectionFromResourceNode
} from '../../src/utils/tileCacheResourceTree'

const parseLocator = (locator) => {
  const match = String(locator || '').match(/^addp:\/\/engine\/(\d+)\/path\/([^?]*)(?:\?(.*))?$/)
  const params = new URLSearchParams(match?.[3] || '')
  return {
    engineId: Number(match?.[1] || 0),
    path: String(match?.[2] || '').split('/').filter(Boolean),
    type: params.get('type') || '',
    nodeId: Number(params.get('node_id') || 0) || undefined,
    itemId: Number(params.get('item_id') || 0) || undefined
  }
}

describe('tileCacheResourceTree', () => {
  it('uses engine-aware catalog root type', () => {
    expect(catalogRootTypeForEngine({ engine_type: 'postgresql' })).toBe('server')
    expect(catalogRootTypeForEngine({ engine_type: 'minio' })).toBe('service')
    expect(createResourceRootNode({ id: 7, name: 'pg', engine_type: 'postgresql' })).toMatchObject({
      locator: 'addp://engine/7/path/?type=server',
      type: 'server'
    })
  })

  it('merges ancestor chain into existing tree and preserves loaded siblings', () => {
    const engine = { id: 7, name: 'pg', engine_type: 'postgresql' }
    const root = {
      ...createResourceRootNode(engine),
      children: [
        {
          id: 'addp://engine/7/path/public?type=schema&node_id=11',
          locator: 'addp://engine/7/path/public?type=schema&node_id=11',
          label: 'public',
          type: 'schema',
          children: [
            {
              id: 'addp://engine/7/path/public/roads?type=table&item_id=21',
              locator: 'addp://engine/7/path/public/roads?type=table&item_id=21',
              label: 'roads',
              type: 'table',
              children: []
            }
          ]
        }
      ]
    }
    const chain = [
      {
        id: 'addp://engine/7/path/?type=server&node_id=10',
        locator: 'addp://engine/7/path/?type=server&node_id=10',
        label: 'pg',
        type: 'server'
      },
      {
        id: 'addp://engine/7/path/public?type=schema&node_id=11',
        locator: 'addp://engine/7/path/public?type=schema&node_id=11',
        label: 'public',
        type: 'schema'
      },
      {
        id: 'addp://engine/7/path/public/buildings?type=table&item_id=22',
        locator: 'addp://engine/7/path/public/buildings?type=table&item_id=22',
        label: 'buildings',
        type: 'table'
      }
    ]

    const merged = mergeAncestorChainIntoResourceTree([root], chain, { engine, parseLocator })

    expect(merged.expandedKeys).toEqual([
      'addp://engine/7/path/?type=server&node_id=10',
      'addp://engine/7/path/public?type=schema&node_id=11'
    ])
    expect(merged.target).toMatchObject({ label: 'buildings', type: 'table' })
    expect(merged.nodes[0].children[0].children.map((node) => node.label)).toEqual(['roads', 'buildings'])
  })

  it('keeps locator source identity when selecting a table resource', () => {
    const locator = 'addp://engine/7/path/public/roads?type=table&item_id=21'

    expect(tableSelectionFromResourceNode({
      locator,
      type: 'table',
      metadata: { item_fingerprint: 'fp-roads' }
    }, parseLocator)).toMatchObject({
      source_engine_id: 7,
      item_id: 21,
      item_fingerprint: 'fp-roads',
      locator,
      source_kind: 'table',
      full_name: 'public/roads',
      schema: 'public',
      table: 'roads'
    })
  })
})
