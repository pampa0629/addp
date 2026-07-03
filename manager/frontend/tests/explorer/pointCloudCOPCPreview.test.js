import { describe, expect, it } from 'vitest'
import {
  collectHierarchyNodeEntries,
  collectHierarchyPageEntries,
  enrichCOPCNodeEntries,
  hierarchyNodeBounds,
  hierarchyDepth,
  hierarchyKeyAncestorOf,
  mergeCOPCNodeSelections,
  pointMaterialSize,
  selectCOPCDetailNodes,
  selectCOPCHierarchyPages,
  selectCOPCOverviewNodes,
  suppressAncestorCOPCNodes
} from '../../src/utils/pointCloudCOPCPreview'

describe('pointCloudCOPCPreview', () => {
  it('sorts COPC overview nodes by shallow hierarchy first', () => {
    const entries = collectHierarchyNodeEntries([
      {
        nodes: {
          '0-0-0-0': { pointCount: 9000, pointDataLength: 100, pointDataOffset: 1 },
          '3-1-1-1': { pointCount: 1000, pointDataLength: 100, pointDataOffset: 2 },
          '5-2-2-2': { pointCount: 100, pointDataLength: 100, pointDataOffset: 3 },
          '5-3-3-3': { pointCount: 200, pointDataLength: 100, pointDataOffset: 4 },
          '6-empty': { pointCount: 0, pointDataLength: 100, pointDataOffset: 5 }
        }
      }
    ])

    const selected = selectCOPCOverviewNodes(entries, 3)

    expect(hierarchyDepth('5-2-2-2')).toBe(5)
    expect(selected).toEqual([
      { key: '0-0-0-0', node: { pointCount: 9000, pointDataLength: 100, pointDataOffset: 1 }, depth: 0 },
      { key: '3-1-1-1', node: { pointCount: 1000, pointDataLength: 100, pointDataOffset: 2 }, depth: 3 },
      { key: '5-3-3-3', node: { pointCount: 200, pointDataLength: 100, pointDataOffset: 4 }, depth: 5 }
    ])
  })

  it('keeps point material size bounded for large scenes', () => {
    expect(pointMaterialSize({ x: 20000, y: 800, z: 100 }, 420000)).toBeLessThanOrEqual(2.2)
    expect(pointMaterialSize({ x: 1, y: 1, z: 1 }, 420000)).toBeGreaterThanOrEqual(0.8)
  })

  it('collects COPC hierarchy pages for on-demand traversal', () => {
    const entries = collectHierarchyPageEntries([
      {
        pages: {
          '2-1-1-1': { pageOffset: 100, pageLength: 64 },
          '3-empty': { pageOffset: 200, pageLength: 0 }
        }
      }
    ])

    expect(entries).toEqual([
      { key: '2-1-1-1', page: { pageOffset: 100, pageLength: 64 }, depth: 2 }
    ])
  })

  it('calculates COPC node bounds from hierarchy keys', () => {
    expect(hierarchyNodeBounds([0, 0, 0, 8, 8, 8], '1-1-0-1')).toEqual([4, 0, 4, 8, 4, 8])
    expect(hierarchyNodeBounds([0, 0, 0, 8, 8, 8], '2-2-1-3')).toEqual([4, 2, 6, 6, 4, 8])
  })

  it('merges overview coverage with camera-driven detail nodes', () => {
    const entries = enrichCOPCNodeEntries(collectHierarchyNodeEntries([
      {
        nodes: {
          '0-0-0-0': { pointCount: 9000, pointDataLength: 100, pointDataOffset: 1 },
          '1-0-0-0': { pointCount: 8000, pointDataLength: 100, pointDataOffset: 2 },
          '4-8-8-8': { pointCount: 1000, pointDataLength: 100, pointDataOffset: 3 },
          '5-16-16-16': { pointCount: 500, pointDataLength: 100, pointDataOffset: 4 }
        }
      }
    ]), [0, 0, 0, 100, 100, 100])

    const overview = selectCOPCOverviewNodes(entries, 2)
    const detail = selectCOPCDetailNodes(entries, {
      camera: { x: 52, y: 52, z: 52 },
      target: { x: 50, y: 50, z: 50 },
      viewportHeight: 800,
      fov: 45
    }, { nodeLimit: 2, pointBudget: 10000 })
    const merged = mergeCOPCNodeSelections(overview, detail)

    expect(merged.some((entry) => entry.key === '0-0-0-0')).toBe(true)
    expect(merged.some((entry) => entry.depth >= 4)).toBe(true)
  })

  it('limits detail selection by point budget', () => {
    const entries = enrichCOPCNodeEntries(collectHierarchyNodeEntries([
      {
        nodes: {
          '4-8-8-8': { pointCount: 6000, pointDataLength: 100, pointDataOffset: 1 },
          '5-16-16-16': { pointCount: 6000, pointDataLength: 100, pointDataOffset: 2 },
          '6-32-32-32': { pointCount: 6000, pointDataLength: 100, pointDataOffset: 3 }
        }
      }
    ]), [0, 0, 0, 100, 100, 100])

    const selected = selectCOPCDetailNodes(entries, {
      camera: { x: 50, y: 50, z: 50 },
      target: { x: 50, y: 50, z: 50 },
      viewportHeight: 800,
      fov: 45
    }, { nodeLimit: 10, pointBudget: 10000, nodePointLimit: 6000 })

    expect(selected.length).toBe(2)
  })

  it('selects nearby hierarchy pages before distant pages', () => {
    const pages = enrichCOPCNodeEntries(collectHierarchyPageEntries([
      {
        pages: {
          '4-8-8-8': { pageOffset: 100, pageLength: 64 },
          '4-1-1-1': { pageOffset: 200, pageLength: 64 }
        }
      }
    ]), [0, 0, 0, 100, 100, 100])

    const selected = selectCOPCHierarchyPages(pages, {
      camera: { x: 52, y: 52, z: 52 },
      target: { x: 50, y: 50, z: 50 },
      viewportHeight: 800,
      fov: 45
    }, { pageLimit: 1, minProjectedPixels: 1 })

    expect(selected.map((entry) => entry.key)).toEqual(['4-8-8-8'])
  })

  it('uses estimated render points for detail point budget', () => {
    const entries = enrichCOPCNodeEntries(collectHierarchyNodeEntries([
      {
        nodes: {
          '4-8-8-8': { pointCount: 1000000, pointDataLength: 100, pointDataOffset: 1 },
          '5-16-16-16': { pointCount: 1000000, pointDataLength: 100, pointDataOffset: 2 },
          '6-32-32-32': { pointCount: 1000000, pointDataLength: 100, pointDataOffset: 3 }
        }
      }
    ]), [0, 0, 0, 100, 100, 100])

    const selected = selectCOPCDetailNodes(entries, {
      camera: { x: 50, y: 50, z: 50 },
      target: { x: 50, y: 50, z: 50 },
      viewportHeight: 800,
      fov: 45
    }, { nodeLimit: 10, pointBudget: 160000, nodePointLimit: 80000 })

    expect(selected.length).toBe(2)
  })

  it('assigns larger render limits to screen-dominant detail nodes', () => {
    const entries = enrichCOPCNodeEntries(collectHierarchyNodeEntries([
      {
        nodes: {
          '4-8-8-8': { pointCount: 1000000, pointDataLength: 100, pointDataOffset: 1 },
          '4-1-1-1': { pointCount: 1000000, pointDataLength: 100, pointDataOffset: 2 }
        }
      }
    ]), [0, 0, 0, 100, 100, 100])

    const selected = selectCOPCDetailNodes(entries, {
      camera: { x: 52, y: 52, z: 52 },
      target: { x: 50, y: 50, z: 50 },
      viewportHeight: 800,
      fov: 45
    }, { nodeLimit: 2, pointBudget: 240000, nodePointLimit: 120000, minNodePointLimit: 24000 })

    expect(selected[0].key).toBe('4-8-8-8')
    expect(selected[0].renderPointLimit).toBeGreaterThanOrEqual(selected[1].renderPointLimit)
    expect(selected[0].estimatedRenderPoints).toBeGreaterThanOrEqual(24000)
  })

  it('prefers loaded deep descendants over large projected ancestors', () => {
    const entries = enrichCOPCNodeEntries(collectHierarchyNodeEntries([
      {
        nodes: {
          '4-8-8-8': { pointCount: 1000000, pointDataLength: 100, pointDataOffset: 1 },
          '9-260-260-260': { pointCount: 120000, pointDataLength: 100, pointDataOffset: 2 },
          '10-520-520-520': { pointCount: 90000, pointDataLength: 100, pointDataOffset: 3 }
        }
      }
    ]), [0, 0, 0, 100, 100, 100])

    const selected = selectCOPCDetailNodes(entries, {
      camera: { x: 50.5, y: 50.5, z: 50.5 },
      target: { x: 50.4, y: 50.4, z: 50.4 },
      viewportHeight: 900,
      fov: 45
    }, { nodeLimit: 2, pointBudget: 300000, nodePointLimit: 180000 })

    expect(selected.map((entry) => entry.key)).toEqual(['10-520-520-520', '9-260-260-260'])
  })

  it('suppresses overview ancestors when detail children are selected', () => {
    const overview = [
      { key: '0-0-0-0' },
      { key: '2-1-1-1' },
      { key: '2-3-3-3' }
    ]
    const detail = [
      { key: '4-4-4-4' }
    ]

    expect(hierarchyKeyAncestorOf('2-1-1-1', '4-4-4-4')).toBe(true)
    expect(suppressAncestorCOPCNodes(overview, detail).map((entry) => entry.key)).toEqual(['2-3-3-3'])
  })
})
