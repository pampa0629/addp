import client from './client'

const endpoint = '/system/platform/host_nodes'

export const hostNodesAPI = {
  list: params => client.get(endpoint, { params }),
  get: nodeID => client.get(`${endpoint}/${encodeURIComponent(nodeID)}`),
  create: body => client.post(endpoint, body),
  update: (nodeID, body) => client.put(`${endpoint}/${encodeURIComponent(nodeID)}`, body)
}
