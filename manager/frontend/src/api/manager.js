import client from './client'

export const managerAPI = {
  // 获取数据源列表
  getDataSources(page = 1, pageSize = 10) {
    return client.get('/datasources', {
      params: { page, pageSize }
    })
  },

  // 获取数据源详情
  getDataSource(id) {
    return client.get(`/datasources/${id}`)
  },

  // 同步数据源（从 System 模块的 resources 同步）
  syncDataSources() {
    return client.post('/datasources/sync')
  },

  // 删除数据源
  deleteDataSource(id) {
    return client.delete(`/datasources/${id}`)
  },

  // 测试数据源连接
  testDataSource(id) {
    return client.post(`/datasources/${id}/test`)
  },

  // 获取目录列表
  getDirectories(params) {
    return client.get('/directories', { params })
  },

  // 创建目录
  createDirectory(data) {
    return client.post('/directories', data)
  },

  // 更新目录
  updateDirectory(id, data) {
    return client.put(`/directories/${id}`, data)
  },

  // 删除目录
  deleteDirectory(id) {
    return client.delete(`/directories/${id}`)
  },

  // 预览数据
  previewData(params) {
    return client.get('/preview', { params })
  },

  // 上传文件
  uploadFile(formData, config) {
    return client.post('/upload', formData, config)
  },

  // 元数据扫描和管理
  // 扫描数据源元数据
  scanDataSource(dataSourceId) {
    return client.post(`/datasources/${dataSourceId}/scan`)
  },

  // 获取数据源的表列表
  getTables(dataSourceId, isManaged = null) {
    const params = {}
    if (isManaged !== null) {
      params.managed = isManaged
    }
    return client.get(`/datasources/${dataSourceId}/tables`, { params })
  },

  // 纳管表（提取详细元数据）
  manageTable(tableId) {
    return client.post(`/tables/${tableId}/manage`)
  },

  // 取消纳管表
  unmanageTable(tableId) {
    return client.post(`/tables/${tableId}/unmanage`)
  }
}
