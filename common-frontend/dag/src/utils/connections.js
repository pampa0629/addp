function itemModel(item) {
  return item?.getModel?.() || item || null
}

export function getDAGIncomingEdgeModels(graph, targetId) {
  if (!targetId) return []
  return (graph?.getEdges?.() || [])
    .map(itemModel)
    .filter(edge => edge?.target === targetId)
}

export function getDAGUpstreamCandidates({ graph, targetId, hasLoop } = {}) {
  if (!targetId) return []
  const connectedSourceIds = new Set(
    getDAGIncomingEdgeModels(graph, targetId).map(edge => edge.source)
  )

  return (graph?.getNodes?.() || [])
    .map(itemModel)
    .filter(node => node?.id && node.id !== targetId)
    .map(node => ({
      node,
      connected: connectedSourceIds.has(node.id),
      disabled: !connectedSourceIds.has(node.id) && Boolean(hasLoop?.(node.id, targetId))
    }))
}
