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

  // F5b: 从 Neo4j 引擎推导本体（不依赖知识图谱）
  listNeo4jEngines() {
    return client.get('/graph/ontologies/neo4j-engines')
  },
  inferSchemaFromEngine(engineId, ontologyId) {
    const params = `engine_id=${engineId}${ontologyId ? `&ontology_id=${ontologyId}` : ''}`
    return client.get(`/graph/ontologies/infer-schema/from-engine?${params}`)
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
  // 获取 Neo4j 引擎列表
  getNeo4jEngines() {
    return client.get('/system/engines').then(res => {
      const engines = Array.isArray(res) ? res : (res?.engines || res?.data || [])
      return engines.filter(e =>
        e.engine_type?.toLowerCase() === 'neo4j' ||
        e.type?.toLowerCase() === 'neo4j'
      )
    })
  },
  // 获取指定引擎的数据库列表（Neo4j database 对应顶层 catalog 节点）
  getDatabases(engineId) {
    return client.post(`/system/engines/${engineId}/catalog/children`, {
      path: { segments: [] },
      options: {}
    }).then(res => {
      const nodes = Array.isArray(res?.nodes) ? res.nodes : (Array.isArray(res?.data?.nodes) ? res.data.nodes : [])
      return nodes
        .filter(node => node.is_container)
        .map(node => node.name)
    })
  }
}
