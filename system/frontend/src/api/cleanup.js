import client from './client'

export const cleanupApi = {
  // 创建评估任务
  createScanTask: (data) => {
    return client.post('/system/admin/cleanup/scan', data)
  },

  // 创建执行任务
  createExecuteTask: (data) => {
    return client.post('/system/admin/cleanup/execute', data)
  },

  // 获取任务状态
  getTaskStatus: (taskId) => {
    return client.get(`/system/admin/cleanup/tasks/${taskId}`)
  },

  // 获取任务历史
  getTaskHistory: (params) => {
    return client.get('/system/admin/cleanup/history', { params })
  }
}
