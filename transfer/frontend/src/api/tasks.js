import client from './client'

// 任务管理 API
export const taskAPI = {
  // 获取任务列表
  list(params) {
    return client.get('/transfer/tasks', { params })
  },

  // 获取任务详情
  get(id) {
    return client.get(`/transfer/tasks/${id}`)
  },

  // 创建任务
  create(data) {
    return client.post('/transfer/tasks', data)
  },

  // 更新任务
  update(id, data) {
    return client.put(`/transfer/tasks/${id}`, data)
  },

  // 删除任务
  delete(id) {
    return client.delete(`/transfer/tasks/${id}`)
  },

  // 启动任务
  start(id) {
    return client.post(`/transfer/tasks/${id}/start`)
  },

  // 停止任务
  stop(id) {
    return client.post(`/transfer/tasks/${id}/stop`)
  },

  // 暂停任务
  pause(id) {
    return client.post(`/transfer/tasks/${id}/pause`)
  },

  // 恢复任务
  resume(id) {
    return client.post(`/transfer/tasks/${id}/resume`)
  },

  // 获取任务统计
  statistics() {
    return client.get('/transfer/tasks/statistics')
  },

  // 获取任务的执行记录
  executions(id, params) {
    return client.get(`/transfer/tasks/${id}/executions`, { params })
  },

  // 字段映射写入任务 config.transforms[]，不再提供独立 mappings API。
}

// 执行记录 API
export const executionAPI = {
  // 获取执行记录列表
  list(params) {
    return client.get('/transfer/executions', { params })
  },

  // 获取执行详情
  get(id) {
    return client.get(`/transfer/executions/${id}`)
  },

  // 取消执行
  cancel(id) {
    return client.post(`/transfer/executions/${id}/cancel`)
  },

  // 重试执行
  retry(id) {
    return client.post(`/transfer/executions/${id}/retry`)
  },

  // 获取执行进度
  progress(id) {
    return client.get(`/transfer/executions/${id}/progress`)
  },

  // 获取执行日志
  logs(id, params) {
    return client.get(`/transfer/executions/${id}/logs`, { params })
  },

  // 获取执行统计
  statistics(params) {
    return client.get('/transfer/executions/statistics', { params })
  }
}
