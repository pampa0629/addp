import client from './client'

export default {
  // 查询服务管理 API (内部数据服务发布)

  // 获取服务列表
  listServices(params) {
    return client.get('/service/query', { params })
  },

  // 搜索服务
  searchServices(params) {
    return client.get('/service/query', { params })
  },

  // 获取服务详情
  getService(id) {
    return client.get(`/service/query/${id}`)
  },

  // 创建服务
  createService(data) {
    return client.post('/service/query', data)
  },

  // 更新服务
  updateService(id, data) {
    return client.put(`/service/query/${id}`, data)
  },

  // 删除服务
  deleteService(id) {
    return client.delete(`/service/query/${id}`)
  },

  // 图层管理 API (查询服务暂时不支持多图层，这些接口待实现)

  // 添加图层
  addLayer(serviceId, data) {
    return client.post(`/service/query/${serviceId}/layers`, data)
  },

  // 更新图层
  updateLayer(serviceId, layerId, data) {
    return client.put(`/service/query/${serviceId}/layers/${layerId}`, data)
  },

  // 删除图层
  deleteLayer(serviceId, layerId) {
    return client.delete(`/service/query/${serviceId}/layers/${layerId}`)
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
