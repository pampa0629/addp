import client from './client'

export const managerAPI = {
  // 获取数据源列表
  getDataSources(page = 1, pageSize = 10) {
    return client.get('/manager/datasources', {
      params: { page, pageSize }
    })
  },

  // 获取数据源详情
  getDataSource(id) {
    return client.get(`/manager/datasources/${id}`)
  },

  // 同步数据源（从 System 模块的 engines 同步）
  syncDataSources() {
    return client.post('/manager/datasources/sync')
  },

  // 删除数据源
  deleteDataSource(id) {
    return client.delete(`/manager/datasources/${id}`)
  },

  // 测试数据源连接
  testDataSource(id) {
    return client.post(`/manager/datasources/${id}/test`)
  },

  // 获取目录列表
  getDirectories(params) {
    return client.get('/manager/directories', { params })
  },

  // 创建目录
  createDirectory(data) {
    return client.post('/manager/directories', data)
  },

  // 更新目录
  updateDirectory(id, data) {
    return client.put(`/manager/directories/${id}`, data)
  },

  // 删除目录
  deleteDirectory(id) {
    return client.delete(`/manager/directories/${id}`)
  },

  // 预览数据
  previewData(params) {
    return client.get('/manager/preview', { params })
  },

  // 上传文件
  uploadFile(formData, config) {
    return client.post('/manager/upload', formData, config)
  },

  // 元数据扫描和管理
  // 扫描数据源元数据
  scanDataSource(dataSourceId) {
    return client.post(`/manager/datasources/${dataSourceId}/scan`)
  },

  // 扫描任务管理
  getScanTasks(dataSourceId) {
    return client.get(`/manager/datasources/${dataSourceId}/scan-tasks`)
  },

  createScanTask(dataSourceId, payload) {
    return client.post(`/manager/datasources/${dataSourceId}/scan-tasks`, payload)
  },

  updateScanTask(dataSourceId, taskId, payload) {
    return client.put(`/manager/datasources/${dataSourceId}/scan-tasks/${taskId}`, payload)
  },

  deleteScanTask(dataSourceId, taskId) {
    return client.delete(`/manager/datasources/${dataSourceId}/scan-tasks/${taskId}`)
  },

  triggerScanTask(dataSourceId, taskId) {
    return client.post(`/manager/datasources/${dataSourceId}/scan-tasks/${taskId}/trigger`)
  },

  createManualScanRun(dataSourceId, payload) {
    return client.post(`/manager/datasources/${dataSourceId}/scan-runs/manual`, payload)
  },

  getScanRuns(dataSourceId, params = {}) {
    return client.get(`/manager/datasources/${dataSourceId}/scan-runs`, { params })
  },

  getScanRun(dataSourceId, runId) {
    return client.get(`/manager/datasources/${dataSourceId}/scan-runs/${runId}`)
  },

  // 获取数据源的表列表
  getTables(dataSourceId, isManaged = null) {
    const params = {}
    if (isManaged !== null) {
      params.managed = isManaged
    }
    return client.get(`/manager/datasources/${dataSourceId}/tables`, { params })
  },

  // 纳管表（提取详细元数据）
  manageTable(tableId) {
    return client.post(`/manager/tables/${tableId}/manage`)
  },

  // 取消纳管表
  unmanageTable(tableId) {
    return client.post(`/manager/tables/${tableId}/unmanage`)
  }
}
