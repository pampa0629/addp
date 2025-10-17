import client from './client'

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
    return client.get('/data-explorer/preview', { params })
  }
}

export default dataExplorerAPI
