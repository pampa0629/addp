import client from './client'
import {
  listSystemCatalogChildren,
  normalizeCatalogPath
} from './metaCatalog'
import {
  selectLiveCatalogTopEntries,
  selectScannedCatalogTopEntries
} from '../utils/catalogScanView'

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

  // 获取引擎列表（从系统控制面）
  getResources() {
    return client.get('/meta/engines')
  },

  // 获取指定引擎已扫描的 catalog 顶层业务项。
  getScannedCatalogTopEntries(engineId, resource) {
    return client
      .get(`/meta/engines/${engineId}/tree`)
      .then(tree => selectScannedCatalogTopEntries(tree, resource))
  },

  // 获取指定引擎的实时 catalog 子节点（系统控制面）
  listCatalogChildren(engineId, path = { segments: [] }, options = {}) {
    return listSystemCatalogChildren(client, engineId, path, options)
  },

  // 获取扫描配置使用的实时 catalog 顶层业务项。
  async listCatalogTopEntries(engineId, resource) {
    const roots = await listSystemCatalogChildren(client, engineId)
    const root = roots.find(isCatalogRootEntry)
    if (!root) {
      return []
    }
    const children = await listSystemCatalogChildren(client, engineId, normalizeCatalogPath(root.path))
    return selectLiveCatalogTopEntries(children, resource).map(toCatalogBrowserNode)
  },

  // 为所有未扫描的引擎提交手动后台扫描运行
  createUnscannedScanRuns() {
    return client.post('/meta/scan/run/unscanned')
  },

  // 提交后台扫描运行
  createManualScanRun(engineId, catalogPaths, options = {}) {
    const payload = {
      engine_id: engineId,
      scan_depth: options.scan_depth || 'deep',
      force: options.force === true,
      trigger_type: 'manual',
      source: options.source || 'meta_frontend'
    }
    if (catalogPaths && catalogPaths.length > 0) {
      payload.catalog_paths = catalogPaths
    }
    return client.post('/meta/scan/run/manual', payload)
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

  getScanRun(runId) {
    return client.get(`/meta/executions/${runId}`)
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

function toCatalogBrowserNode(node) {
  const nodePath = catalogPathToString(node.path) || node.name
  const type = catalogNodeBrowserType(node)
  return {
    name: node.name,
    schema_name: node.name,
    path: nodePath,
    catalog_path: node.path,
    term: node.term,
    kind: node.kind,
    role: node.role,
    type,
    node_type: type,
    is_container: node.role === 'branch',
    is_item: node.role === 'leaf',
    size_bytes: node.storage?.size_bytes ?? node.table?.size_bytes,
    table: node.table,
    storage: node.storage,
    leaf_count: node.leaf_count,
    updated_at: node.updated_at,
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
  if (node.kind === 'namespace') {
    return node.term || 'namespace'
  }
  if (node.role === 'branch') {
    return node.term || 'prefix'
  }
  return node.term || 'object'
}

function fileExtension(name = '') {
  const index = name.lastIndexOf('.')
  return index >= 0 ? name.slice(index) : ''
}


function isCatalogRootEntry(node) {
  if (!node) return false
  const segments = Array.isArray(node.path?.segments) ? node.path.segments : []
  return segments.length === 1 && ['server', 'service', 'root'].includes(String(node.term || '').toLowerCase())
}
