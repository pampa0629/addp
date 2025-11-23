import client from './client'

// 请求取消控制器（防止重复请求导致性能问题）
let previewAbortController = null

export const dataExplorerAPI = {
  getResources() {
    return client.get('/data-explorer/resources')
  },
  getTree(resourceId) {
    return client.get(`/data-explorer/resources/${resourceId}/tree`)
  },
  getLegacyTree() {
    return client.get('/data-explorer/tree')
  },
  refreshNode(resourceId, payload) {
    return client.post(`/data-explorer/resources/${resourceId}/refresh`, payload)
  },
  getPreview(params) {
    // 取消之前未完成的预览请求
    if (previewAbortController) {
      previewAbortController.abort()
    }

    // 创建新的取消控制器
    previewAbortController = new AbortController()

    return client.get('/data-explorer/preview', {
      params,
      signal: previewAbortController.signal,
      timeout: 60000 // 空间数据预览可能需要更长时间（60秒）
    })
  },
  // 获取要素的几何中心点（用于表格行定位到地图）
  getFeatureCentroid(resourceId, schema, table, featureId, geom = 'geom', primaryKey = 'id') {
    return client.get(`/resources/${resourceId}/features/${featureId}/centroid`, {
      params: { schema, table, geom, primary_key: primaryKey }
    })
  }
}

export default dataExplorerAPI
