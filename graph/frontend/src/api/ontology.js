import client from './client'

export const ontologyAPI = {
  list() {
    return client.get('/graph/ontologies')
  },
  get(id) {
    return client.get(`/graph/ontologies/${id}`)
  },
  create(data) {
    return client.post('/graph/ontologies', data)
  },
  update(id, data) {
    return client.put(`/graph/ontologies/${id}`, data)
  },
  delete(id) {
    return client.delete(`/graph/ontologies/${id}`)
  },

  // 实体类型
  listEntityTypes(ontologyId) {
    return client.get(`/graph/ontologies/${ontologyId}/entity-types`)
  },
  createEntityType(ontologyId, data) {
    return client.post(`/graph/ontologies/${ontologyId}/entity-types`, data)
  },
  updateEntityType(ontologyId, id, data) {
    return client.put(`/graph/ontologies/${ontologyId}/entity-types/${id}`, data)
  },
  deleteEntityType(ontologyId, id) {
    return client.delete(`/graph/ontologies/${ontologyId}/entity-types/${id}`)
  },

  // 关系类型
  listRelationTypes(ontologyId) {
    return client.get(`/graph/ontologies/${ontologyId}/relation-types`)
  },
  createRelationType(ontologyId, data) {
    return client.post(`/graph/ontologies/${ontologyId}/relation-types`, data)
  },
  updateRelationType(ontologyId, id, data) {
    return client.put(`/graph/ontologies/${ontologyId}/relation-types/${id}`, data)
  },
  deleteRelationType(ontologyId, id) {
    return client.delete(`/graph/ontologies/${ontologyId}/relation-types/${id}`)
  },

  // 版本
  listVersions(ontologyId) {
    return client.get(`/graph/ontologies/${ontologyId}/versions`)
  },
  createVersion(ontologyId, data) {
    return client.post(`/graph/ontologies/${ontologyId}/versions`, data)
  },

  // 约束同步
  syncEntityTypeConstraints(ontologyId, entityTypeId, graphId) {
    return client.post(`/graph/ontologies/${ontologyId}/entity-types/${entityTypeId}/sync-constraints`, { graph_id: graphId })
  },

  // 空间图层同步
  syncEntityTypeSpatialLayer(ontologyId, entityTypeId, graphId) {
    return client.put(`/graph/ontologies/${ontologyId}/entity-types/${entityTypeId}/sync-spatial-layer`, { graph_id: graphId })
  },

  // F4: 从 Model 模块导入本体
  getImportPreviewFromModel() {
    return client.get('/graph/ontologies/import-preview/from-model')
  },
  importFromModel(ontologyId, data) {
    return client.post(`/graph/ontologies/${ontologyId}/import-from-model`, data)
  },

  // Explicit business graph target, shared by both graph engines.
  inferSchemaFromEngine(target, ontologyId) {
    return client.post('/graph/ontologies/infer-schema/from-engine', { ...target, ontology_id: ontologyId })
  },
  applyInferredSchemaFromEngine(ontologyId, data) {
    return client.post(`/graph/ontologies/${ontologyId}/infer-schema/from-engine/apply`, data)
  }
}

export const knowledgeGraphAPI = {
  list() {
    return client.get('/graph/graphs')
  },
  get(id) {
    return client.get(`/graph/graphs/${id}`)
  },
  create(data) {
    return client.post('/graph/graphs', data)
  },
  update(id, data) {
    return client.put(`/graph/graphs/${id}`, data)
  },
  delete(id) {
    return client.delete(`/graph/graphs/${id}`)
  }
}

export const engineAPI = {
  async getGraphEngines() {
    const groups = await Promise.all(['neo4j', 'falkordb'].map(engine_type =>
      client.get('/system/engine-catalog/engines', { params: { engine_type } })
    ))
    return groups.flat()
  },
  async getGraphTargets(engineId) {
    const children = async path => {
      const response = await client.post(`/system/engines/${engineId}/catalog/children`, { path, options: {} })
      return response.nodes
    }
    const graphLeaves = async path => {
      const nodes = await children(path)
      const groups = await Promise.all(nodes.map(node => {
        if (node.role === 'leaf') return node.kind === 'graph' ? [node] : []
        return node.role === 'branch' ? graphLeaves(node.path) : []
      }))
      return groups.flat()
    }
    return graphLeaves({ segments: [] })
  }
}
