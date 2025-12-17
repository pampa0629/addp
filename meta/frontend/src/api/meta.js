import client from './client'

export default {
  // 数据源相关
  getDatasources() {
    return client.get('/meta/datasources')
  },

  getDatasource(id) {
    return client.get(`/meta/datasources/${id}`)
  },

  // 数据库相关
  getDatabases(datasourceId) {
    return client.get(`/meta/datasources/${datasourceId}/databases`)
  },

  getDatabase(id) {
    return client.get(`/meta/databases/${id}`)
  },

  // 表相关
  getTables(databaseId) {
    return client.get(`/meta/databases/${databaseId}/tables`)
  },

  getTable(id) {
    return client.get(`/meta/tables/${id}`)
  },

  // 字段相关
  getFields(tableId) {
    return client.get(`/meta/tables/${tableId}/fields`)
  },

  // 同步相关
  syncResource(resourceId) {
    return client.post(`/meta/sync/${resourceId}`)
  },

  autoSyncAll() {
    return client.post('/meta/sync/auto')
  },

  // 扫描相关
  deepScanDatabase(databaseId) {
    return client.post(`/meta/scan/database/${databaseId}`)
  },

  deepScanTable(tableId) {
    return client.post(`/meta/scan/table/${tableId}`)
  },

  // 搜索
  searchTables(keyword) {
    return client.get('/meta/search/tables', { params: { keyword } })
  },

  searchFields(keyword) {
    return client.get('/meta/search/fields', { params: { keyword } })
  },

  // 统计
  getStats() {
    return client.get('/meta/stats')
  },

  // 同步日志
  getSyncLogs(params) {
    return client.get('/meta/logs', { params })
  },

  // 元数据扫描（旧API，保留兼容）
  getSchemasOld(resourceId) {
    return client.get(`/meta/scan/schemas/${resourceId}`)
  },

  scanMetadata(data) {
    return client.post('/meta/scan', data)
  },

  // ===== 新的元数据扫描API（对应router_new.go） =====

  // 获取资源列表（从System模块）
  getResources() {
    return client.get('/meta/resources')
  },

  // 获取指定资源的Schema列表
  getSchemas(resourceId) {
    return client.get(`/meta/schemas/${resourceId}`)
  },

  // 获取指定资源的可用Schema列表（未扫描的）
  listAvailableSchemas(resourceId) {
    return client.get(`/meta/schemas/${resourceId}/available`)
  },

  // 自动扫描所有未扫描的资源
  autoScan() {
    return client.post('/meta/scan/auto')
  },

  // 扫描指定资源的指定Schema
  scanResource(resourceId, schemaNames) {
    return client.post('/meta/scan/resource', {
      resource_id: resourceId,
      schema_names: schemaNames
    })
  },

  getScanRuns(resourceId, params = {}) {
    return client.get('/meta/scan/runs', { params }).then(res => {
      const data = res?.data ?? res
      const items = Array.isArray(data) ? data : data.items || []
      const total = data.total || items.length || 0
      if (!resourceId) {
        return { items, total }
      }
      const filtered = items.filter(run => run.resource_id === resourceId)
      return { items: filtered, total: data.total || filtered.length || 0 }
    })
  },

  getScanTasks(resourceId) {
    return client.get('/meta/scan/tasks').then(res => {
      const tasks = res?.data ?? []
      if (!resourceId) {
        return tasks
      }
      return tasks.filter(task => task.resource_id === resourceId)
    })
  },

  createScanTask(resourceId, payload) {
    return client
      .post('/meta/scan/tasks', {
        ...payload,
        resource_id: resourceId
      })
      .then(res => res?.data ?? res)
  },

  updateScanTask(resourceId, taskId, payload) {
    return client
      .put(`/meta/scan/tasks/${taskId}`, {
        ...payload,
        resource_id: resourceId
      })
      .then(res => res?.data ?? res)
  }
}
