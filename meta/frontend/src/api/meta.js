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

  // 获取指定引擎的实时 catalog 子节点（System 统一控制面）
  listCatalogChildren(engineId, path = { segments: [] }, options = {}) {
    const catalogPath = typeof path === 'string'
      ? browserPathToCatalogPath(path)
      : { segments: [], ...path }
    return client.post(`/system/engines/${engineId}/catalog/children`, {
      path: catalogPath,
      options
    }).then(res => Array.isArray(res?.nodes) ? res.nodes : [])
  },

  // 获取扫描配置使用的实时 catalog 顶层节点
  listCatalogTopNodes(engineId) {
    return this.listCatalogChildren(engineId).then(nodes => nodes.map(toBrowserNode))
  },

  // 迁移期兼容：对象存储/文件系统浏览统一转到 System catalog API
  listObjectStorageNodes(engineId, path = '') {
    return this.listCatalogChildren(engineId, path).then(nodes => nodes.map(toBrowserNode))
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

function browserPathToCatalogPath(path = '') {
  const segments = String(path)
    .split('/')
    .map(part => part.trim())
    .filter(Boolean)
    .map((name, index) => ({
      name,
      term: index === 0 ? 'bucket' : 'prefix',
      kind: index === 0 ? 'bucket' : 'prefix'
    }))
  return { segments }
}

function toBrowserNode(node) {
  const nodePath = node.attributes?.path || catalogPathToString(node.path) || node.name
  const type = catalogNodeBrowserType(node)
  return {
    name: node.name,
    schema_name: node.name,
    path: nodePath,
    type,
    node_type: type,
    size_bytes: node.stats?.size_bytes,
    file_type: type === 'file' || type === 'object' ? fileExtension(node.name) : ''
  }
}

function catalogPathToString(path) {
  const segments = Array.isArray(path?.segments) ? path.segments : []
  return segments.map(segment => segment.name).filter(Boolean).join('/')
}

function catalogNodeBrowserType(node) {
  if (['bucket', 'root', 'prefix', 'object', 'file'].includes(node.kind)) {
    return node.kind
  }
  return node.is_container ? 'prefix' : 'object'
}

function fileExtension(name = '') {
  const index = name.lastIndexOf('.')
  return index >= 0 ? name.slice(index) : ''
}
