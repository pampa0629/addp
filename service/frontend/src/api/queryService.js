import client from './client'
import queryExecutionClient from './queryExecutionClient'

export default {
  // 查询服务管理 API

  // 获取查询服务列表
  listServices(params) {
    return client.get('/service/query', { params })
  },

  // 搜索查询服务
  searchServices(params) {
    return client.get('/service/query', { params })
  },

  // 获取查询服务详情
  getService(id) {
    return client.get(`/service/query/${id}`)
  },

  // 创建查询服务
  createService(data) {
    return client.post('/service/query', data)
  },

  // 更新查询服务
  updateService(id, data) {
    return client.put(`/service/query/${id}`, data)
  },

  // 删除查询服务
  deleteService(id) {
    return client.delete(`/service/query/${id}`)
  },

  checkSourceSnapshot(id) {
    return client.get(`/service/query/${id}/source-snapshot-diff`)
  },

  refreshSourceSnapshot(id) {
    return client.post(`/service/query/${id}/refresh-source-snapshot`)
  },

  // REST 查询端点测试 API

  // 测试查询端点（Table 模式）
  testQuery(serviceName, params) {
    return queryExecutionClient.get(`/api/query/${serviceName}`, { params })
  },

  // 测试查询端点（导出 CSV）
  testQueryCSV(serviceName, params) {
    return queryExecutionClient.get(`/api/query/${serviceName}`, {
      params: { ...params, format: 'csv' },
      responseType: 'blob'
    })
  },

  // 测试查询端点（导出 GeoJSON）
  testQueryGeoJSON(serviceName, params) {
    return queryExecutionClient.get(`/api/query/${serviceName}`, {
      params: { ...params, format: 'geojson' }
    })
  },

  // OGC API Features 端点测试 API

  // 测试 OGC Features Landing Page
  testOGCFeaturesLanding(serviceName) {
    return queryExecutionClient.get(`/ogc/features/${serviceName}`)
  },

  // 测试 OGC Features Conformance
  testOGCFeaturesConformance(serviceName) {
    return queryExecutionClient.get(`/ogc/features/${serviceName}/conformance`)
  },

  // 测试 OGC Features Collections
  testOGCFeaturesCollections(serviceName) {
    return queryExecutionClient.get(`/ogc/features/${serviceName}/collections`)
  },

  // 测试 OGC Features Items
  testOGCFeaturesItems(serviceName, collectionId, params) {
    return queryExecutionClient.get(`/ogc/features/${serviceName}/collections/${collectionId}/items`, { params })
  },

  // 测试 OGC Features Single Item
  testOGCFeaturesItem(serviceName, collectionId, featureId) {
    return queryExecutionClient.get(`/ogc/features/${serviceName}/collections/${collectionId}/items/${featureId}`)
  },

  // 数据源相关 API

  // 检测 SQL 查询结果的空间元数据
  detectSQLOutputContract(data) {
    return client.post('/service/sql/output-contract', data)
  },

  getQueryEngineSample(engineId) {
    return client.get(`/service/query-engines/${engineId}/sample-query`)
  },

  // 跨模块 API: 获取存储引擎列表（来自 System 模块）
  getStorageEngines() {
    return client.get('/system/engines')
  }
}
