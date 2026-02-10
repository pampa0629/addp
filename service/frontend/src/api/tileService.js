import client from './client'

export default {
  // 瓦片服务管理 API

  // 获取瓦片服务列表
  listServices(params) {
    return client.get('/service/tile', { params })
  },

  // 搜索瓦片服务
  searchServices(params) {
    return client.get('/service/tile', { params })
  },

  // 获取瓦片服务详情
  getService(id) {
    return client.get(`/service/tile/${id}`)
  },

  // 创建瓦片服务
  createService(data) {
    return client.post('/service/tile', data)
  },

  // 更新瓦片服务
  updateService(id, data) {
    return client.put(`/service/tile/${id}`, data)
  },

  // 删除瓦片服务
  deleteService(id) {
    return client.delete(`/service/tile/${id}`)
  },

  // 图层管理 API

  // 添加图层
  addLayer(serviceId, data) {
    return client.post(`/service/tile/${serviceId}/layers`, data)
  },

  // 更新图层
  updateLayer(serviceId, layerId, data) {
    return client.put(`/service/tile/${serviceId}/layers/${layerId}`, data)
  },

  // 删除图层
  deleteLayer(serviceId, layerId) {
    return client.delete(`/service/tile/${serviceId}/layers/${layerId}`)
  },

  // 清理图层缓存
  clearLayerCache(serviceId, layerId) {
    return client.delete(`/service/tile/${serviceId}/layers/${layerId}/cache`)
  },

  // 瓦片端点测试 API

  // 测试 XYZ Tiles 端点
  testXYZTile(serviceName, layerName, z, x, y, format = 'mvt') {
    return client.get(`/tiles/${serviceName}/${layerName}/${z}/${x}/${y}.${format}`, {
      responseType: 'arraybuffer'
    })
  },

  // 测试 WMTS GetCapabilities
  testWMTSCapabilities(serviceName) {
    return client.get(`/wmts/${serviceName}`)
  },

  // 测试 OGC Tiles Landing Page
  testOGCTilesLanding(serviceName) {
    return client.get(`/ogc/tiles/${serviceName}`)
  },

  // 测试 OGC Tiles Conformance
  testOGCTilesConformance(serviceName) {
    return client.get(`/ogc/tiles/${serviceName}/conformance`)
  },

  // 测试 OGC Tiles TileMatrixSets
  testOGCTilesTileMatrixSets(serviceName) {
    return client.get(`/ogc/tiles/${serviceName}/tileMatrixSets`)
  },

  // 测试 OGC Tiles Tilesets
  testOGCTilesTilesets(serviceName) {
    return client.get(`/ogc/tiles/${serviceName}/tiles`)
  },

  // 测试 OGC Tiles GetTile
  testOGCTile(serviceName, layerName, tileMatrixSetId, z, row, col) {
    return client.get(`/ogc/tiles/${serviceName}/tiles/${layerName}/${tileMatrixSetId}/${z}/${row}/${col}`, {
      responseType: 'arraybuffer'
    })
  },

  // 跨模块 API: 获取存储引擎列表（来自 System 模块）
  getStorageEngines() {
    return client.get('/system/engines')
  }
}
