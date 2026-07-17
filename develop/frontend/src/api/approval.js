import client from './client'

export const getToolApproval = (id) => client.get(`/develop/approvals/${id}`)

export const decideToolApproval = (id, decision) => (
  client.post(`/develop/approvals/${id}/decision`, { decision })
)
