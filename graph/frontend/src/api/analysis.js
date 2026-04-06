import client from './client'

export const analysisAPI = {
  getCapabilities(graphId) {
    return client.get(`/graph/graphs/${graphId}/analysis/capabilities`)
  },
  runAlgorithm(graphId, params) {
    return client.post(`/graph/graphs/${graphId}/analysis/run`, params)
  }
}
