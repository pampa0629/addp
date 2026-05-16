import client from './client'

// 请求取消控制器（防止重复请求导致性能问题）
let previewAbortController = null

export const dataExplorerAPI = {
  // 获取可用引擎列表（Manager只查询引擎，不管理引擎）
  getEngines() {
    return client.get('/manager/engines')
  },
  getTree(engineId, expandDepth = 2) {
    return client.get(`/manager/tree/${engineId}`, {
      params: { expand_depth: expandDepth }
    })
  },

  // 获取节点的子节点（增量加载）
  getNodeChildren(engineId, locator, expandDepth = 1) {
    return client.get(`/manager/tree/${engineId}/node`, {
      params: { locator, expand_depth: expandDepth }
    })
  },

  // 搜索资源树节点
  searchNodes(engineId, keyword, nodeTypes = null, limit = 50) {
    const params = { q: keyword, limit }
    if (nodeTypes) {
      params.node_types = Array.isArray(nodeTypes) ? nodeTypes.join(',') : nodeTypes
    }
    return client.get(`/manager/tree/${engineId}/search`, { params })
  },

  refreshNode(engineId, locator) {
    return client.post(`/manager/tree/${engineId}/refresh`, { locator })
  },
  getPreview(locator, page = 1, pageSize = 50, childName = '', componentPath = '', nestedChildPath = '') {
    // 取消之前未完成的预览请求
    if (previewAbortController) {
      previewAbortController.abort()
    }

    // 创建新的取消控制器
    previewAbortController = new AbortController()

    const params = { locator, page, page_size: pageSize }
    if (childName) {
      params.child_name = childName
    }
    if (componentPath) {
      params.component_path = componentPath
    }
    if (nestedChildPath) {
      params.nested_child_path = nestedChildPath
    }

    return client.get('/manager/preview', {
      params,
      signal: previewAbortController.signal,
      timeout: 60000 // 空间数据预览可能需要更长时间（60秒）
    })
  },
  // 获取要素的几何中心点（用于表格行定位到地图）
  // 注意：这是空间数据服务路由，不是引擎管理
  getFeatureCentroid(engineId, schema, table, featureId, geom = 'geom', primaryKey = 'id') {
    return client.get(`/manager/engines/${engineId}/spatial/features/${featureId}/centroid`, {
      params: { schema, table, geom, primary_key: primaryKey }
    })
  },
  // 获取要素的完整几何（用于地图高亮显示）
  getFeatureGeometry(engineId, schema, table, featureId, geom = 'geom', primaryKey = 'id') {
    return client.get(`/manager/engines/${engineId}/spatial/features/${featureId}/geometry`, {
      params: { schema, table, geom, primary_key: primaryKey }
    })
  }
}

export default dataExplorerAPI
