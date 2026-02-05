import client from './client'

export default {
  // 注册服务管理 API

  // 获取注册服务列表
  listServices(params) {
    return client.get('/service/registered', { params })
  },

  // 搜索注册服务
  searchServices(params) {
    return client.get('/service/registered', { params })
  },

  // 获取注册服务详情
  getService(id) {
    return client.get(`/service/registered/${id}`)
  },

  // 创建注册服务
  createService(data) {
    return client.post('/service/registered', data)
  },

  // 更新注册服务
  updateService(id, data) {
    return client.put(`/service/registered/${id}`, data)
  },

  // 删除注册服务
  deleteService(id) {
    return client.delete(`/service/registered/${id}`)
  },

  // 刷新服务元数据
  refreshMetadata(id, data) {
    return client.post(`/service/registered/${id}/refresh`, data)
  },

  // 执行健康检查
  healthCheck(id) {
    return client.post(`/service/registered/${id}/health`)
  },

  // 代理转发到外部服务
  proxyService(id, path, params) {
    return client.get(`/service/proxy/${id}/${path}`, { params })
  }
}
