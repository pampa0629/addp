import client from './client'

// 任务管理 API
export const taskAPI = {
  // 获取任务列表
  list(params) {
    return client.get('/transfer/tasks', { params })
  },

  // 获取任务详情
  get(id) {
    return client.get(`/transfer/task-definitions/${id}`)
  },

  // 创建任务
  create(data) {
    return client.post('/transfer/task-definitions', data)
  },

  // 更新任务
  update(id, data) {
    return client.put(`/transfer/task-definitions/${id}`, data)
  },

  // 删除任务
  delete(id) {
    return client.delete(`/transfer/task-definitions/${id}`)
  },

  // 启动任务
  start(id) {
    return client.post(`/transfer/task-definitions/${id}/start`)
  },

  // 暂停任务
  pause(id) {
    return client.post(`/transfer/task-definitions/${id}/pause`)
  },

  // 恢复任务
  resume(id) {
    return client.post(`/transfer/task-definitions/${id}/resume`)
  },

  // 停止持续同步任务
  stop(id, data) {
    return client.post(`/transfer/task-definitions/${id}/stop`, data)
  },

  // 获取任务统计
  statistics() {
    return client.get('/transfer/task-definitions/statistics')
  },

  // 获取任务的执行记录
  executions(id, params) {
    return client.get(`/transfer/task-definitions/${id}/executions`, { params })
  },

  // 获取任务的 DLQ 安全控制索引
  deadLetters(id, params) {
    return client.get(`/transfer/task-definitions/${id}/dead-letters`, { params })
  },

  // 获取单条 DLQ 安全控制索引详情
  deadLetter(id, identity) {
    return client.get(`/transfer/task-definitions/${id}/dead-letters/${identity}`)
  },

  // 创建写入新 PostgreSQL 隔离表的 bounded replay execution
  replay(id, data) {
    return client.post(`/transfer/task-definitions/${id}/replay`, data)
  },

  schemaChange(id) {
    return client.get(`/transfer/task-definitions/${id}/schema-change`)
  },

  approveSchemaChange(id, data) {
    return client.post(`/transfer/task-definitions/${id}/schema-change/approve`, data)
  },

  // 字段映射写入任务 config.transforms[]，不再提供独立 mappings API。
}

export const fieldDefinitionRecommendationAPI = {
  create(data) {
    return client.post('/transfer/field-definition-recommendations', data)
  }
}

// 执行记录 API
export const executionAPI = {
  // 获取执行记录列表
  list(params) {
    return client.get('/transfer/executions', { params })
  },

  // 获取执行详情
  get(executionId) {
    return client.get(`/transfer/executions/${executionId}`)
  },

  // 重试执行
  retry(executionId) {
    return client.post(`/transfer/executions/${executionId}/retry`)
  },

  // 获取执行进度
  progress(executionId) {
    return client.get(`/transfer/executions/${executionId}/progress`)
  },

  // 获取执行日志
  logs(executionId, params) {
    return client.get(`/transfer/executions/${executionId}/logs`, { params })
  },

  // 获取执行统计
  statistics(params) {
    return client.get('/transfer/executions/statistics', { params })
  }
}
