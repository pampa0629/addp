import client from './client'

export const searchAPI = {
  fullText(params) {
    return client.get('/search/fulltext', { params })
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
