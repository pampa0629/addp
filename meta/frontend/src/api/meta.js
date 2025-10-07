import client from './client'

export default {
  // 数据源相关
  getDatasources() {
    return client.get('/api/meta/datasources')
  },

  getDatasource(id) {
    return client.get(`/api/meta/datasources/${id}`)
  },

  // 数据库相关
  getDatabases(datasourceId) {
    return client.get(`/api/meta/datasources/${datasourceId}/databases`)
  },

  getDatabase(id) {
    return client.get(`/api/meta/databases/${id}`)
  },

  // 表相关
  getTables(databaseId) {
    return client.get(`/api/meta/databases/${databaseId}/tables`)
  },

  getTable(id) {
    return client.get(`/api/meta/tables/${id}`)
  },

  // 字段相关
  getFields(tableId) {
    return client.get(`/api/meta/tables/${tableId}/fields`)
  },

  // 同步相关
  syncResource(resourceId) {
    return client.post(`/api/meta/sync/${resourceId}`)
  },

  autoSyncAll() {
    return client.post('/api/meta/sync/auto')
  },

  // 扫描相关
  deepScanDatabase(databaseId) {
    return client.post(`/api/meta/scan/database/${databaseId}`)
  },

  deepScanTable(tableId) {
    return client.post(`/api/meta/scan/table/${tableId}`)
  },

  // 搜索
  searchTables(keyword) {
    return client.get('/api/meta/search/tables', { params: { keyword } })
  },

  searchFields(keyword) {
    return client.get('/api/meta/search/fields', { params: { keyword } })
  },

  // 统计
  getStats() {
    return client.get('/api/meta/stats')
  },

  // 同步日志
  getSyncLogs(params) {
    return client.get('/api/meta/logs', { params })
  },

  // 元数据扫描（旧API，保留兼容）
  getSchemasOld(resourceId) {
    return client.get(`/api/meta/scan/schemas/${resourceId}`)
  },

  scanMetadata(data) {
    return client.post('/api/meta/scan', data)
  },

  // ===== 新的元数据扫描API（对应router_new.go） =====

  // 获取资源列表（从System模块）
  getResources() {
    return client.get('/api/meta/resources')
  },

  // 获取指定资源的Schema列表
  getSchemas(resourceId) {
    return client.get(`/api/meta/schemas/${resourceId}`)
  },

  // 获取指定资源的可用Schema列表（未扫描的）
  listAvailableSchemas(resourceId) {
    return client.get(`/api/meta/schemas/${resourceId}/available`)
  },

  // 自动扫描所有未扫描的资源
  autoScan() {
    return client.post('/api/meta/scan/auto')
  },

  // 扫描指定资源的指定Schema
  scanResource(resourceId, schemaNames) {
    return client.post('/api/meta/scan/resource', {
      resource_id: resourceId,
      schema_names: schemaNames
    })
  }
}
