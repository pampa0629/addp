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

// 只改变渲染透明度，不改写图模型或持久化布局。
export function focusDAGConnections(graph, item) {
  if (!graph) return []
  const nodes = graph.getNodes()
  const edges = graph.getEdges()
  const active = item && !item.destroyed ? item : null
  const model = active?.getModel()
  const focusedEdges = active ? edges.filter(edge => {
    const connection = edge.getModel()
    return active.getType() === 'edge'
      ? edge === active
      : connection.source === model.id || connection.target === model.id
  }) : []
  const nodeIds = new Set(active?.getType() === 'node' ? [model.id] : [])
  focusedEdges.forEach(edge => {
    nodeIds.add(edge.getModel().source)
    nodeIds.add(edge.getModel().target)
  })
  nodes.forEach(node => node.getContainer().attr('opacity', !active || nodeIds.has(node.getID()) ? 1 : 0.25))
  edges.forEach(edge => {
    const focused = focusedEdges.includes(edge)
    edge.getContainer().attr('opacity', !active || focused ? 1 : 0.12)
    if (focused) edge.toFront()
  })
  graph.paint()
  return focusedEdges
}
