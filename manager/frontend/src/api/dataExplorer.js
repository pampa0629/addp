import client from './client'

export const dataExplorerAPI = {
  getTree() {
    return client.get('/data-explorer/tree')
  },
  getPreview(params) {
    return client.get('/data-explorer/preview', { params })
  }
}

export default dataExplorerAPI
