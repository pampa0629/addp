// Model 模块 API（仅包含 Model 模块自己的功能）
import client from './client'

// ========== 业务域 API（通过后端代理到 Standard 模块）==========
// 注意：这些 API 实际上由 Standard 模块提供，Model 后端仅做代理转发
export const domainAPI = {
  // 获取业务域列表（树形）
  list() {
    return client.get('/model/domains')
  }
}

// ========== 数据元 API（通过后端代理到 Standard 模块）==========
export const elementAPI = {
  // 获取数据元列表
  list(params) {
    return client.get('/model/elements', { params })
  }
}

// ========== 数仓分层 API ==========
export const dwLayerAPI = {
  // 获取数仓分层列表
  list() {
    return client.get('/model/dw-layers')
  },
  // 创建数仓分层
  create(data) {
    return client.post('/model/dw-layers', data)
  },
  // 获取数仓分层详情
  get(id) {
    return client.get(`/model/dw-layers/${id}`)
  },
  // 更新数仓分层
  update(id, data) {
    return client.put(`/model/dw-layers/${id}`, data)
  },
  // 删除数仓分层
  delete(id) {
    return client.delete(`/model/dw-layers/${id}`)
  }
}

// ========== 业务实体 API ==========
export const entityAPI = {
  // 获取业务实体列表
  list(params) {
    return client.get('/model/entities', { params })
  },
  // 创建业务实体
  create(data) {
    return client.post('/model/entities', data)
  },
  // 获取业务实体详情
  get(id) {
    return client.get(`/model/entities/${id}`)
  },
  // 更新业务实体
  update(id, data) {
    return client.put(`/model/entities/${id}`, data)
  },
  // 删除业务实体
  delete(id) {
    return client.delete(`/model/entities/${id}`)
  },
  // 审批通过
  approve(id) {
    return client.post(`/model/entities/${id}/approve`)
  },
  // 获取实体属性列表
  getAttributes(entityId) {
    return client.get(`/model/entities/${entityId}/attributes`)
  },
  // 创建实体属性
  createAttribute(entityId, data) {
    return client.post(`/model/entities/${entityId}/attributes`, data)
  },
  // 更新实体属性
  updateAttribute(entityId, attrId, data) {
    return client.put(`/model/entities/${entityId}/attributes/${attrId}`, data)
  },
  // 删除实体属性
  deleteAttribute(entityId, attrId) {
    return client.delete(`/model/entities/${entityId}/attributes/${attrId}`)
  },
  // 导入 Mermaid ER 图
  importMermaid(data) {
    return client.post('/model/entities/import-mermaid', data)
  },
  // 导出 Mermaid ER 图
  exportMermaid() {
    return client.get('/model/entities/export-mermaid', { responseType: 'text' })
  }
}

// ========== 实体关系 API ==========
export const entityRelationAPI = {
  // 获取实体关系列表
  list(params) {
    return client.get('/model/entity-relations', { params })
  },
  // 根据实体ID获取关系列表
  getByEntityId(entityId) {
    return client.get('/model/entity-relations', { params: { entity_id: entityId } })
  },
  // 创建实体关系
  create(data) {
    return client.post('/model/entity-relations', data)
  },
  // 获取实体关系详情
  get(id) {
    return client.get(`/model/entity-relations/${id}`)
  },
  // 更新实体关系
  update(id, data) {
    return client.put(`/model/entity-relations/${id}`, data)
  },
  // 删除实体关系
  delete(id) {
    return client.delete(`/model/entity-relations/${id}`)
  }
}

// ========== 逻辑表 API ==========
export const logicalTableAPI = {
  // 获取逻辑表列表
  list(params) {
    return client.get('/model/logical-tables', { params })
  },
  // 创建逻辑表
  create(data) {
    return client.post('/model/logical-tables', data)
  },
  // 获取逻辑表详情
  get(id) {
    return client.get(`/model/logical-tables/${id}`)
  },
  // 更新逻辑表
  update(id, data) {
    return client.put(`/model/logical-tables/${id}`, data)
  },
  // 删除逻辑表
  delete(id) {
    return client.delete(`/model/logical-tables/${id}`)
  },
  // 获取逻辑表字段列表
  getFields(tableId) {
    return client.get(`/model/logical-tables/${tableId}/fields`)
  },
  // 创建字段
  createField(tableId, data) {
    return client.post(`/model/logical-tables/${tableId}/fields`, data)
  },
  // 更新字段
  updateField(tableId, fieldId, data) {
    return client.put(`/model/logical-tables/${tableId}/fields/${fieldId}`, data)
  },
  // 删除字段
  deleteField(tableId, fieldId) {
    return client.delete(`/model/logical-tables/${tableId}/fields/${fieldId}`)
  },
  // 预览 DDL
  previewDDL(id) {
    return client.post(`/model/logical-tables/${id}/preview-ddl`)
  },
  // 获取事实表关联的指标列表
  listMetrics(tableId) {
    return client.get(`/model/logical-tables/${tableId}/metrics`)
  },
  // 关联指标到事实表
  addMetric(tableId, data) {
    return client.post(`/model/logical-tables/${tableId}/metrics`, data)
  },
  // 解除指标关联
  removeMetric(tableId, mappingId) {
    return client.delete(`/model/logical-tables/${tableId}/metrics/${mappingId}`)
  },
  // 获取事实表关联的维度表列表
  listDimensionRelations(tableId) {
    return client.get(`/model/logical-tables/${tableId}/dimension-relations`)
  },
  // 添加维度表关联
  addDimensionRelation(tableId, data) {
    return client.post(`/model/logical-tables/${tableId}/dimension-relations`, data)
  },
  // 删除维度表关联
  removeDimensionRelation(tableId, relationId) {
    return client.delete(`/model/logical-tables/${tableId}/dimension-relations/${relationId}`)
  }
}

// ========== 维度层级 API（通过后端代理到 Standard 模块）==========
export const dimensionHierarchyAPI = {
  list(params) {
    return client.get('/model/dimension-hierarchies', { params })
  },
  get(id) {
    return client.get(`/model/dimension-hierarchies/${id}`)
  }
}

// ========== Standard 指标 API（通过后端代理，用于搜索可关联的指标）==========
export const standardMetricAPI = {
  list(params) {
    return client.get('/model/standard-metrics', { params })
  }
}
