import client from './client'

// 请求取消控制器（防止重复请求导致性能问题）
let previewAbortController = null

export const dataExplorerAPI = {
  // 获取可用引擎列表（Manager只查询引擎，不管理引擎）
  getEngines() {
    return client.get('/manager/engines')
  },
  getTree(engineId, expandDepth = 2) {
    return client.get(`/meta/resource-tree/${engineId}`, {
      params: { expand_depth: expandDepth }
    })
  },

  // 获取节点的子节点（增量加载）
  getNodeChildren(engineId, locator) {
    return client.get(`/meta/resource-tree/${engineId}/node`, {
      params: { locator }
    })
  },

  // 获取资源树祖先链（用于任意深度 locator 定位）
  getTreeAncestors(engineId, locator) {
    return client.get(`/meta/resource-tree/${engineId}/ancestors`, {
      params: { locator }
    })
  },

  // 搜索资源树节点
  searchNodes(engineId, keyword, nodeTypes = null, limit = 50) {
    const params = { q: keyword, limit }
    if (nodeTypes) {
      params.node_types = Array.isArray(nodeTypes) ? nodeTypes.join(',') : nodeTypes
    }
    return client.get(`/meta/resource-tree/${engineId}/search`, { params })
  },

  refreshNode(engineId, locator) {
    return client.post(`/meta/resource-tree/${engineId}/refresh`, null, {
      params: { locator }
    })
  },
  getPreview(locator, page = 1, pageSize = 50, childName = '', refPath = '', nestedChildPath = '') {
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
    if (refPath) {
      params.ref_path = refPath
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
  getDataProfileCurrent(locator, selection = {}, profileConfigHash = '') {
    const params = { locator }
    if (selection.childName) params.child_name = selection.childName
    if (selection.refPath) params.ref_path = selection.refPath
    if (selection.nestedChildPath) params.nested_child_path = selection.nestedChildPath
    if (profileConfigHash) params.profile_config_hash = profileConfigHash
    return client.get('/manager/data-profiles/current', { params })
  },
  createDataProfileExecution(locator, selection = {}, dataScope = { kind: 'all' }) {
    const payload = { locator, mode: 'sample', data_scope: dataScope }
    if (selection.childName) payload.child_name = selection.childName
    if (selection.refPath) payload.ref_path = selection.refPath
    if (selection.nestedChildPath) payload.nested_child_path = selection.nestedChildPath
    return client.post('/manager/data-profile-executions', payload)
  },
  getResourceActions(locator) {
    return client.get('/manager/resource-actions', {
      params: { locator }
    })
  },
  uploadFiles(formData) {
    return client.post('/manager/uploads', formData, {
      headers: { 'Content-Type': 'multipart/form-data' }
    })
  },
  createExport(payload) {
    return client.post('/manager/exports', payload)
  },
  getExport(id) {
    return client.get(`/manager/exports/${id}`)
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
