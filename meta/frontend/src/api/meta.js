import client from './client'
import {
  listCatalogChildren as listSystemCatalogChildren,
  normalizeCatalogPath,
  toCatalogBrowserNode
} from '@common-ui'

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

  // 获取指定引擎已扫描的 catalog 顶层容器
  getScannedCatalogTopNodes(engineId) {
    return client.get(`/meta/engines/${engineId}/tree`).then(res => {
      const root = findCatalogRootNode(res)
      const childNodes = Array.isArray(res?.child_nodes) ? res.child_nodes : []
      const nodes = root
        ? childNodes.filter(node => node.parent_node_id === root.id)
        : []
      return nodes.map(node => ({
        id: node.id,
        name: node.name,
        node_type: node.node_type,
        path: node.full_name || node.name,
        scan_status: node.scan_status,
        scanned_depth: node.scanned_depth,
        scanned_at: node.scanned_at,
        item_count: node.item_count || 0,
        total_size_bytes: node.total_size_bytes || 0
      }))
    })
  },

  // 获取指定引擎的实时 catalog 子节点（System 统一控制面）
  listCatalogChildren(engineId, path = { segments: [] }, options = {}) {
    return listSystemCatalogChildren(client, engineId, path, options)
  },

  // 获取扫描配置使用的实时 catalog 顶层节点
  async listCatalogTopNodes(engineId) {
    const roots = await listSystemCatalogChildren(client, engineId)
    const root = roots.find(isCatalogRootEntry)
    if (!root) {
      return []
    }
    const children = await listSystemCatalogChildren(client, engineId, normalizeCatalogPath(root.path))
    return children
      .filter(node => node?.role === 'branch')
      .map(toCatalogBrowserNode)
  },

  // 自动扫描所有未扫描的引擎
  autoScan() {
    return client.post('/meta/scan/auto')
  },

  // 提交后台扫描运行
  createManualScanRun(engineId, catalogPaths, options = {}) {
    const payload = {
      engine_id: engineId,
      scan_depth: options.scan_depth || 'deep',
      force: options.force === true,
      trigger_type: 'manual'
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
    return client.get(`/meta/scan/runs/${runId}`)
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

function findCatalogRootNode(tree) {
  const roots = Array.isArray(tree?.top_nodes) ? tree.top_nodes : []
  return roots.find(node => String(node?.full_name || '').trim() === '')
}

function isCatalogRootEntry(node) {
  if (!node) return false
  const segments = Array.isArray(node.path?.segments) ? node.path.segments : []
  return segments.length === 1 && ['server', 'service', 'root'].includes(String(node.term || '').toLowerCase())
}
