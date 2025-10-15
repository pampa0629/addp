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
  getPreview(params) {
    return client.get('/data-explorer/preview', { params })
  }
}

export default dataExplorerAPI
