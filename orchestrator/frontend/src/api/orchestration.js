import client from './client'

export default {
  // 获取编排列表
  list() {
    return client.get('/orchestrator/orchestrations')
  },

  // 获取编排详情
  get(id) {
    return client.get(`/orchestrator/orchestrations/${id}`)
  },

  // 创建编排
  create(data) {
    return client.post('/orchestrator/orchestrations', data)
  },

  // 更新编排
  update(id, data) {
    return client.put(`/orchestrator/orchestrations/${id}`, data)
  },

  // 删除编排
  delete(id) {
    return client.delete(`/orchestrator/orchestrations/${id}`)
  },

  // 手动触发执行
  execute(id) {
    return client.post(`/orchestrator/orchestrations/${id}/execute`)
  },

  // 获取执行列表
  listExecutions(id, params) {
    return client.get(`/orchestrator/orchestrations/${id}/executions`, { params })
  },

  // 获取所有执行记录
  listAllExecutions(params) {
    return client.get('/orchestrator/executions', { params })
  },

  // 获取执行详情
  getExecution(id) {
    return client.get(`/orchestrator/orch-executions/${id}`)
  }
}
