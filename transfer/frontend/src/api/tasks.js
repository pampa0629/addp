import client from './client'

// 任务管理 API
export const taskAPI = {
  // 获取任务列表
  list(params) {
    return client.get('/tasks', { params })
  },

  // 获取任务详情
  get(id) {
    return client.get(`/tasks/${id}`)
  },

  // 创建任务
  create(data) {
    return client.post('/tasks', data)
  },

  // 更新任务
  update(id, data) {
    return client.put(`/tasks/${id}`, data)
  },

  // 删除任务
  delete(id) {
    return client.delete(`/tasks/${id}`)
  },

  // 启动任务
  start(id) {
    return client.post(`/tasks/${id}/start`)
  },

  // 停止任务
  stop(id) {
    return client.post(`/tasks/${id}/stop`)
  },

  // 暂停任务
  pause(id) {
    return client.post(`/tasks/${id}/pause`)
  },

  // 恢复任务
  resume(id) {
    return client.post(`/tasks/${id}/resume`)
  },

  // 获取任务统计
  statistics() {
    return client.get('/tasks/statistics')
  },

  // 获取任务的执行记录
  executions(id, params) {
    return client.get(`/tasks/${id}/executions`, { params })
  },

  // 获取任务的字段映射
  getMappings(id) {
    return client.get(`/tasks/${id}/mappings`)
  },

  // 创建字段映射
  createMapping(id, data) {
    return client.post(`/tasks/${id}/mappings`, data)
  },

  // 删除字段映射
  deleteMapping(mappingId) {
    return client.delete(`/mappings/${mappingId}`)
  }
}

// 执行记录 API
export const executionAPI = {
  // 获取执行记录列表
  list(params) {
    return client.get('/executions', { params })
  },

  // 获取执行详情
  get(id) {
    return client.get(`/executions/${id}`)
  },

  // 取消执行
  cancel(id) {
    return client.post(`/executions/${id}/cancel`)
  },

  // 重试执行
  retry(id) {
    return client.post(`/executions/${id}/retry`)
  },

  // 获取执行进度
  progress(id) {
    return client.get(`/executions/${id}/progress`)
  },

  // 获取执行日志
  logs(id, params) {
    return client.get(`/executions/${id}/logs`, { params })
  },

  // 获取执行统计
  statistics(params) {
    return client.get('/executions/statistics', { params })
  }
}
