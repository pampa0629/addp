import client from './client'

export const searchAPI = {
  // 混合检索（全文检索 + 向量检索）
  search(params) {
    return client.get('/search', { params })
  },

  history(params) {
    return client.get('/search/history', { params })
  },

  deleteHistoryItem(id) {
    return client.delete(`/search/history/${id}`)
  },

  clearHistory() {
    return client.delete('/search/history')
  }
}

export default searchAPI
