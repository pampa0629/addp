import apiClient from './client'

/**
 * 获取所有血缘关系
 */
export const getAllLineage = () => {
  return apiClient.get('/lineage/all')
}

/**
 * 获取指定节点的上游依赖
 * @param {string} itemId - 节点ID
 */
export const getUpstream = (itemId) => {
  return apiClient.get(`/lineage/upstream/${itemId}`)
}

/**
 * 获取指定节点的下游依赖
 * @param {string} itemId - 节点ID
 */
export const getDownstream = (itemId) => {
  return apiClient.get(`/lineage/downstream/${itemId}`)
}

/**
 * 获取完整血缘图谱
 * @param {string} itemId - 节点ID
 * @param {number} maxDepth - 最大深度，默认3
 */
export const getLineageGraph = (itemId, maxDepth = 3) => {
  return apiClient.get(`/lineage/graph/${itemId}`, {
    params: { max_depth: maxDepth }
  })
}

/**
 * 获取工作流血缘
 * @param {string} workflowId - 工作流ID
 */
export const getWorkflowLineage = (workflowId) => {
  return apiClient.get(`/lineage/workflow/${workflowId}`)
}
