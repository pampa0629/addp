import client from './client'

export const oauthAPI = {
  authorize: (payload) => client.post('/system/oauth/authorizations', payload),
  approveDevice: (payload) => client.post('/system/oauth/device/authorizations', payload)
}
