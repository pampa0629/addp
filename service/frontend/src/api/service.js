import client from './client'

export default {
  // 注册服务管理 (外部服务注册)
  // 获取服务列表
  list(params) {
    return client.get('/service/registered', { params })
  },

  // 获取服务详情
  get(id) {
    return client.get(`/service/registered/${id}`)
  },

  // 创建服务
  create(data) {
    return client.post('/service/registered', data)
  },

  // 更新服务
  update(id, data) {
    return client.put(`/service/registered/${id}`, data)
  },

  // 删除服务
  delete(id) {
    return client.delete(`/service/registered/${id}`)
  },

  // 刷新元数据
  refreshMetadata(id) {
    return client.post(`/service/registered/${id}/refresh`)
  },

  // 健康检查
  healthCheck(id) {
    return client.post(`/service/registered/${id}/health`)
  },

  // 搜索服务 (暂时保留,待确认后端是否提供)
  search(params) {
    return client.get('/service/registered', { params })
  },

  // 导出配置 (暂时保留,待确认后端是否提供)
  export() {
    return client.get('/service/registered/export')
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
