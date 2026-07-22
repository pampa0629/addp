import client from './client'

export const oauthAPI = {
  getAuthorizationRequest: (requestId) => client.get(`/system/oauth/authorization_requests/${encodeURIComponent(requestId)}`),
  authorize: (payload) => client.post('/system/oauth/authorizations', payload),
  approveDevice: (payload) => client.post('/system/oauth/device/authorizations', payload)
}
