import client from './client'

export const searchAPI = {
  // 混合检索（全文检索 + 向量检索）
  search(params) {
    return client.get('/manager/search', { params })
  },

  history(params) {
    return client.get('/manager/search/history', { params })
  },

  deleteHistoryItem(id) {
    return client.delete(`/manager/search/history/${id}`)
  },

  clearHistory() {
    return client.delete('/manager/search/history')
  }
}

export default searchAPI
