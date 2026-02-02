import client from './client'

export default {
  // 内部服务管理 API

  // 获取服务列表
  listServices(params) {
    return client.get('/service/internal/services', { params })
  },

  // 搜索服务
  searchServices(params) {
    return client.get('/service/internal/services', { params })
  },

  // 获取服务详情
  getService(id) {
    return client.get(`/service/internal/services/${id}`)
  },

  // 创建服务
  createService(data) {
    return client.post('/service/internal/services', data)
  },

  // 更新服务
  updateService(id, data) {
    return client.put(`/service/internal/services/${id}`, data)
  },

  // 删除服务
  deleteService(id) {
    return client.delete(`/service/internal/services/${id}`)
  },

  // 图层管理 API

  // 添加图层
  addLayer(serviceId, data) {
    return client.post(`/service/internal/services/${serviceId}/layers`, data)
  },

  // 更新图层
  updateLayer(serviceId, layerId, data) {
    return client.put(`/service/internal/services/${serviceId}/layers/${layerId}`, data)
  },

  // 删除图层
  deleteLayer(serviceId, layerId) {
    return client.delete(`/service/internal/services/${serviceId}/layers/${layerId}`)
  },

  // OGC 端点测试 API

  // 测试 WFS GetCapabilities
  testWFSCapabilities(serviceName) {
    return client.get(`/ogc/wfs/${serviceName}`, {
      params: { service: 'WFS', request: 'GetCapabilities' }
    })
  },

  // 测试 OGC API Landing Page
  testOGCAPILanding(serviceName) {
    return client.get(`/ogc/api/${serviceName}/`)
  },

  // 测试 OGC API Collections
  testOGCAPICollections(serviceName) {
    return client.get(`/ogc/api/${serviceName}/collections`)
  },

  // 测试 WFS GetFeature
  testWFSGetFeature(serviceName, params) {
    return client.get(`/ogc/wfs/${serviceName}`, {
      params: { service: 'WFS', request: 'GetFeature', ...params }
    })
  },

  // 测试 WMTS GetCapabilities
  testWMTSCapabilities(serviceName) {
    return client.get(`/ogc/wmts/${serviceName}`, {
      params: { service: 'WMTS', request: 'GetCapabilities' }
    })
  },

  // 获取 WMTS 瓦片 URL
  getWMTSTileUrl(serviceName, layer, z, x, y) {
    return `/ogc/wmts/${serviceName}/tile/${layer}/${z}/${x}/${y}.mvt`
  }
}
