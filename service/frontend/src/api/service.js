import client from './client'

export default {
  // 服务注册管理
  // 获取服务列表
  list(params) {
    return client.get('/service/registry/services', { params })
  },

  // 获取服务详情
  get(id) {
    return client.get(`/service/registry/services/${id}`)
  },

  // 创建服务
  create(data) {
    return client.post('/service/registry/services', data)
  },

  // 更新服务
  update(id, data) {
    return client.put(`/service/registry/services/${id}`, data)
  },

  // 删除服务
  delete(id) {
    return client.delete(`/service/registry/services/${id}`)
  },

  // 刷新元数据
  refreshMetadata(id) {
    return client.post(`/service/registry/services/${id}/refresh`)
  },

  // 健康检查
  healthCheck(id) {
    return client.post(`/service/registry/services/${id}/health`)
  },

  // 搜索服务
  search(params) {
    return client.get('/service/registry/search', { params })
  },

  // 导出配置
  export() {
    return client.get('/service/registry/export')
  },

  // 获取服务目录
  getCatalog() {
    return client.get('/service/catalog')
  },

  // 服务代理
  proxy(id, path, params) {
    return client.get(`/service/proxy/${id}/${path}`, { params })
  }
}
