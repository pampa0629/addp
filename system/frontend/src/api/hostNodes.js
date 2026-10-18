import client from './client'

const endpoint = '/system/platform/host_nodes'

export const hostNodesAPI = {
  list: (params, config) => client.get(endpoint, { ...config, params }),
  get: (nodeID, config) => client.get(`${endpoint}/${encodeURIComponent(nodeID)}`, config),
  create: (body, config) => client.post(endpoint, body, config),
  update: (nodeID, body) => client.put(`${endpoint}/${encodeURIComponent(nodeID)}`, body)
}
