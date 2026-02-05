import client from './client'

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

  // REST 查询端点测试 API

  // 测试查询端点（Table 模式）
  testQuery(serviceName, params) {
    return client.get(`/query/${serviceName}`, { params })
  },

  // 测试查询端点（导出 CSV）
  testQueryCSV(serviceName, params) {
    return client.get(`/query/${serviceName}`, {
      params: { ...params, format: 'csv' },
      responseType: 'blob'
    })
  },

  // 测试查询端点（导出 GeoJSON）
  testQueryGeoJSON(serviceName, params) {
    return client.get(`/query/${serviceName}`, {
      params: { ...params, format: 'geojson' }
    })
  },

  // OGC API Features 端点测试 API

  // 测试 OGC Features Landing Page
  testOGCFeaturesLanding(serviceName) {
    return client.get(`/ogc/features/${serviceName}`)
  },

  // 测试 OGC Features Conformance
  testOGCFeaturesConformance(serviceName) {
    return client.get(`/ogc/features/${serviceName}/conformance`)
  },

  // 测试 OGC Features Collections
  testOGCFeaturesCollections(serviceName) {
    return client.get(`/ogc/features/${serviceName}/collections`)
  },

  // 测试 OGC Features Items
  testOGCFeaturesItems(serviceName, collectionId, params) {
    return client.get(`/ogc/features/${serviceName}/collections/${collectionId}/items`, { params })
  },

  // 测试 OGC Features Single Item
  testOGCFeaturesItem(serviceName, collectionId, featureId) {
    return client.get(`/ogc/features/${serviceName}/collections/${collectionId}/items/${featureId}`)
  }
}
