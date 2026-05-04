import client from './client'

export default {
  // 统计
  getStats() {
    return client.get('/meta/stats')
  },

  // 同步日志
  getSyncLogs(params) {
    return client.get('/meta/logs', { params })
  },

  scanMetadata(data) {
    return client.post('/meta/scan', data)
  },

  // 获取引擎列表（从System模块）
  getResources() {
    return client.get('/meta/engines')
  },

  // 获取指定引擎已扫描的 catalog 顶层节点
  getNamespaces(engineId) {
    return client.get(`/meta/engines/${engineId}/tree`).then(res => {
      const nodes = Array.isArray(res?.top_nodes) ? res.top_nodes : []
      return nodes.map(node => ({
        id: node.id,
        name: node.name,
        schema_name: node.name,
        node_type: node.node_type,
        path: node.path || node.full_name || node.name,
        scan_status: node.scan_status,
        scanned_at: node.scanned_at,
        table_count: node.item_count || 0,
        total_size_bytes: node.total_size_bytes || 0
      }))
    })
  },

  // 获取指定引擎的实时命名空间列表
  listAvailableNamespaces(engineId) {
    return client.get(`/system/engines/${engineId}/namespaces`).then(res => {
      const namespaces = Array.isArray(res?.namespaces) ? res.namespaces : []
      return namespaces.map(item => ({
        ...item,
        schema_name: item.name || item.schema_name,
        name: item.name || item.schema_name
      }))
    })
  },

  // 获取对象存储的节点列表
  listObjectStorageNodes(engineId, path = '') {
    return client.get(`/meta/engines/${engineId}/storage/nodes`, {
      params: { path }
    })
  },

  // 自动扫描所有未扫描的引擎
  autoScan() {
    return client.post('/meta/scan/auto')
  },

  // 扫描指定引擎的指定命名空间或对象路径
  scanEngine(engineId, namespaces, objectPaths) {
    const payload = {
      engine_id: engineId
    }
    if (namespaces && namespaces.length > 0) {
      payload.namespaces = namespaces
    }
    if (objectPaths && objectPaths.length > 0) {
      payload.object_paths = objectPaths
    }
    return client.post('/meta/scan/engine', payload)
  },

  getScanRuns(engineId, params = {}) {
    return client.get('/meta/scan/runs', { params }).then(res => {
      const data = res ?? {}
      const items = Array.isArray(data.items) ? data.items : (Array.isArray(data) ? data : [])
      const total = data.total || items.length || 0
      if (!engineId) {
        return { items, total }
      }
      const filtered = items.filter(run => run.engine_id === engineId)
      return { items: filtered, total: data.total || filtered.length || 0 }
    })
  },

  getScanTasks(engineId) {
    return client.get('/meta/scan/tasks').then(res => {
      const tasks = Array.isArray(res) ? res : []
      if (!engineId) {
        return tasks
      }
      return tasks.filter(task => task.engine_id === engineId)
    })
  },

  createScanTask(engineId, payload) {
    return client
      .post('/meta/scan/tasks', {
        ...payload,
        engine_id: engineId
      })
  },

  updateScanTask(engineId, taskId, payload) {
    return client
      .put(`/meta/scan/tasks/${taskId}`, {
        ...payload,
        engine_id: engineId
      })
  }
}
