import { createOrchestrationAPI } from '@common-ui'
import client from './client'

export default {
  ...createOrchestrationAPI(client),

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

  // 获取执行列表
  listExecutions(id, params) {
    return client.get(`/orchestrator/orchestrations/${id}/executions`, { params })
  },

  // 获取执行详情
  getExecution(id) {
    return client.get(`/orchestrator/orch-executions/${id}`)
  }
}
