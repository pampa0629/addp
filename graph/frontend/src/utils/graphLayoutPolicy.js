const LARGE_NODE_COUNT = 100
const LARGE_EDGE_COUNT = 160

export function isLargeGraph(nodeCount = 0, edgeCount = 0) {
  return nodeCount >= LARGE_NODE_COUNT || edgeCount >= LARGE_EDGE_COUNT
}

export function createGraphLayoutConfig(
  layoutType,
  nodeCount = 0,
  edgeCount = 0,
  focusNodeId = '',
) {
  const large = isLargeGraph(nodeCount, edgeCount)
  let config

  switch (layoutType) {
    case 'dagre':
      config = {
        type: 'dagre',
        rankdir: 'LR',
        nodesep: large ? 18 : 50,
        ranksep: large ? 50 : 90,
        edgeLabelSpace: !large,
      }
      break
    case 'circular':
      config = {
        type: 'circular',
        radius: large ? Math.max(320, Math.ceil(nodeCount * 7)) : 320,
        ordering: large ? null : 'degree',
      }
      break
    case 'radial':
      config = {
        type: 'radial',
        unitRadius: large ? 78 : 130,
        preventOverlap: !large,
        nodeSize: 56,
        maxIteration: large ? 60 : 180,
        maxPreventOverlapIteration: large ? 0 : 60,
        ...(focusNodeId ? { focusNode: focusNodeId } : {}),
      }
      break
    default:
      if (nodeCount > 500) {
        config = {
          type: 'forceAtlas2',
          preventOverlap: false,
          barnesHut: true,
          maxIteration: 180,
        }
        break
      }
      config = {
        type: 'force',
        preventOverlap: !large,
        nodeSize: large ? 36 : 48,
        linkDistance: large ? 100 : 180,
        nodeStrength: large ? -180 : -320,
        edgeStrength: large ? 0.18 : 0.12,
        collideStrength: large ? 0 : 0.8,
        alphaDecay: large ? 0.1 : 0.035,
        ...(large ? { alphaMin: 0.04 } : {}),
      }
  }

  return { ...config, relayoutAtChangeData: false }
}
