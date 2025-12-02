import client from './client'

export default {
  // 获取编排列表
  list() {
    return client.get('/orchestrations')
  },

  // 获取编排详情
  get(id) {
    return client.get(`/orchestrations/${id}`)
  },

  // 创建编排
  create(data) {
    return client.post('/orchestrations', data)
  },

  // 更新编排
  update(id, data) {
    return client.put(`/orchestrations/${id}`, data)
  },

  // 删除编排
  delete(id) {
    return client.delete(`/orchestrations/${id}`)
  },

  // 手动触发执行
  execute(id) {
    return client.post(`/orchestrations/${id}/execute`)
  },

  // 获取执行列表
  listExecutions(id, params) {
    return client.get(`/orchestrations/${id}/executions`, { params })
  },

  // 获取执行详情
  getExecution(id) {
    return client.get(`/orch-executions/${id}`)
  }
}
