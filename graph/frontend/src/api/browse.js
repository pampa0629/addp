import client from './client'

export const browseAPI = {
  getBrowseSnapshot(graphId, signal) {
    return client.get(`/graph/graphs/${graphId}/browse-snapshot`, { signal })
  },
  getConstraints(graphId) {
    return client.get(`/graph/graphs/${graphId}/constraints`)
  },
  searchNodes(graphId, query, limit = 30, signal) {
    return client.post(`/graph/graphs/${graphId}/search`, { query, limit }, { signal })
  },
  expandTarget(graphId, target, depth = 1, nodeLimit = 200, relationshipLimit = 400, signal) {
    return client.post(`/graph/graphs/${graphId}/expand`, {
      target,
      depth,
      node_limit: nodeLimit,
      relationship_limit: relationshipLimit
    }, { signal })
  },
  findPath(graphId, sourceId, targetId, signal) {
    return client.post(`/graph/graphs/${graphId}/path`, { source_id: sourceId, target_id: targetId }, { signal })
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
