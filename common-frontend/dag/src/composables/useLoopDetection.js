/**
 * DAG 循环检测 Composable
 * 使用深度优先搜索 (DFS) 检测有向图中的环
 */
export function useLoopDetection(graph) {
  /**
   * 检测从 sourceId 到 targetId 的连接是否会形成环
   * @param {string} sourceId - 源节点 ID
   * @param {string} targetId - 目标节点 ID
   * @returns {boolean} - 如果会形成环返回 true
   */
  function hasLoop(sourceId, targetId) {
    const edges = graph.value.getEdges()
    const visited = new Set()
    const stack = [targetId]

    while (stack.length > 0) {
      const current = stack.pop()
      if (current === sourceId) return true
      if (visited.has(current)) continue

      visited.add(current)

      edges.forEach(edge => {
        const model = edge.getModel()
        const edgeSourceId = model.source
        const edgeTargetId = model.target

        if (edgeSourceId === current) {
          stack.push(edgeTargetId)
        }
      })
    }

    return false
  }

  return {
    hasLoop
  }
}
