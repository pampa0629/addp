import client from './client'

export const datasourceAPI = {
  // 获取数据源列表
  list(params) {
    return client.get('/manager/datasources', { params })
  },

  // 获取数据源详情
  get(id) {
    return client.get(`/manager/datasources/${id}`)
  },

  // 创建数据源
  create(data) {
    return client.post('/manager/datasources', data)
  },

  // 更新数据源
  update(id, data) {
    return client.put(`/manager/datasources/${id}`, data)
  },

  // 删除数据源
  delete(id) {
    return client.delete(`/manager/datasources/${id}`)
  },

  // 测试连接
  testConnection(id) {
    return client.post(`/manager/datasources/${id}/test`)
  },

  // 同步数据源（从 System 模块的 engines 同步）
  sync() {
    return client.post('/manager/datasources/sync')
  }
}