import client from './client'

export const browseAPI = {
  getSchema(graphId) {
    return client.get(`/graph/graphs/${graphId}/schema`)
  },
  getStats(graphId) {
    return client.get(`/graph/graphs/${graphId}/stats`)
  },
  getOverview(graphId) {
    return client.get(`/graph/graphs/${graphId}/overview`)
  },
  getConstraints(graphId) {
    return client.get(`/graph/graphs/${graphId}/constraints`)
  },
  searchNodes(graphId, query, limit = 30) {
    return client.post(`/graph/graphs/${graphId}/search`, { query, limit })
  },
  expandNode(graphId, nodeId, limit = 100) {
    return client.post(`/graph/graphs/${graphId}/expand`, { node_id: nodeId, limit })
  },
  findPath(graphId, sourceId, targetId) {
    return client.post(`/graph/graphs/${graphId}/path`, { source_id: sourceId, target_id: targetId })
  },
  // F5: Neo4j 推导本体
  inferSchema(graphId, ontologyId) {
    const params = ontologyId ? `?ontology_id=${ontologyId}` : ''
    return client.get(`/graph/graphs/${graphId}/infer-schema${params}`)
  },
  applyInferredSchema(graphId, data) {
    return client.post(`/graph/graphs/${graphId}/infer-schema/apply`, data)
  }
}

