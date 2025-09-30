import client from './client'

export const datasourceAPI = {
  // 获取数据源列表
  list(params) {
    return client.get('/datasources', { params })
  },

  // 获取数据源详情
  get(id) {
    return client.get(`/datasources/${id}`)
  },

  // 创建数据源
  create(data) {
    return client.post('/datasources', data)
  },

  // 更新数据源
  update(id, data) {
    return client.put(`/datasources/${id}`, data)
  },

  // 删除数据源
  delete(id) {
    return client.delete(`/datasources/${id}`)
  },

  // 测试连接
  testConnection(id) {
    return client.post(`/datasources/${id}/test`)
  },

  // 同步数据源（从 System 模块的 resources 同步）
  sync() {
    return client.post('/datasources/sync')
  }
}